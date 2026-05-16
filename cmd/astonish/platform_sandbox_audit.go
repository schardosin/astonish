package astonish

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/schardosin/astonish/pkg/config"
	k8sbackend "github.com/schardosin/astonish/pkg/sandbox/k8s"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// handlePlatformSandboxAudit audits both sandbox PVCs for orphaned data by
// running a remote audit pod in the cluster and diffing the on-disk contents
// against what PostgreSQL tracks.
//
// Flags:
//   --reclaim         Delete orphan directories from PVCs (layers + uppers)
//   --reclaim-pg      Also delete orphan PG layer rows that have no on-disk dir
//   --namespace NS    Override sandbox namespace (default from config)
//   --kubeconfig PATH Override kubeconfig path
//   --grace DURATION  Grace period for orphan detection (default 1h)
func handlePlatformSandboxAudit(args []string) error {
	var (
		reclaim    bool
		reclaimPG  bool
		namespace  string
		kubeconfig string
		grace      = time.Hour
	)

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--reclaim":
			reclaim = true
		case "--reclaim-pg":
			reclaimPG = true
		case "--namespace", "-n":
			if i+1 >= len(args) {
				return fmt.Errorf("--namespace requires a value")
			}
			i++
			namespace = args[i]
		case "--kubeconfig":
			if i+1 >= len(args) {
				return fmt.Errorf("--kubeconfig requires a value")
			}
			i++
			kubeconfig = args[i]
		case "--grace":
			if i+1 >= len(args) {
				return fmt.Errorf("--grace requires a value (e.g. 1h, 30m)")
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return fmt.Errorf("invalid --grace duration: %w", err)
			}
			grace = d
		case "-h", "--help":
			printSandboxAuditUsage()
			return nil
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	// Load app config for PG connection and sandbox settings.
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if !appCfg.Storage.IsPostgres() {
		return fmt.Errorf("sandbox-audit requires storage.backend: postgres")
	}

	// Determine sandbox namespace.
	if namespace == "" {
		if appCfg.Sandbox.Kubernetes.Namespace != "" {
			namespace = appCfg.Sandbox.Kubernetes.Namespace
		} else {
			namespace = "astonish-sandbox"
		}
	}

	// Build K8s client.
	cs, _, err := k8sbackend.NewClientFromOptions(k8sbackend.LoadConfigOptions{
		KubeconfigPath: kubeconfig,
	})
	if err != nil {
		return fmt.Errorf("failed to create K8s client: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println("=== Sandbox Storage Audit ===")
	fmt.Println()

	// --- Phase 1: Run audit pod to list directories on both PVCs ---
	fmt.Printf("Spawning audit pod in namespace %q...\n", namespace)
	layersDirs, uppersDirs, err := runAuditPod(ctx, cs, namespace, appCfg)
	if err != nil {
		return fmt.Errorf("audit pod failed: %w", err)
	}

	// --- Phase 2: Query PG for tracked layers and sessions ---
	pgCfg := appCfg.Storage.Postgres
	_, pgStore, err := pgstore.NewPlatformServices(ctx, pgCfg)
	if err != nil {
		return fmt.Errorf("failed to connect to platform DB: %w", err)
	}
	defer pgStore.Close()

	layers := pgStore.SandboxLayers()
	allLayers, err := layers.ListAll(ctx)
	if err != nil {
		return fmt.Errorf("failed to list layers from PG: %w", err)
	}

	// Build sets.
	pgLayerIDs := make(map[string]bool)
	var unreferencedLayers []pgLayerInfo
	for _, l := range allLayers {
		pgLayerIDs[l.LayerID] = true
		if l.RefCount == 0 && time.Since(l.AddedAt) > grace {
			unreferencedLayers = append(unreferencedLayers, pgLayerInfo{
				LayerID:   l.LayerID,
				SizeBytes: l.SizeBytes,
				AddedAt:   l.AddedAt,
			})
		}
	}

	// Get all known session IDs from all team schemas.
	knownSessionIDs, err := getAllSessionIDs(ctx, pgStore)
	if err != nil {
		return fmt.Errorf("failed to list sessions from PG: %w", err)
	}

	// --- Phase 3: Compute diffs ---
	// Layers PVC: directories on disk that aren't in PG.
	var orphanLayerDirs []string
	var stagingDirs []string
	for _, d := range layersDirs {
		if d == "" || d == "@base" {
			continue // @base is always valid
		}
		if strings.HasPrefix(d, "__staging-") {
			stagingDirs = append(stagingDirs, d)
			continue
		}
		if !pgLayerIDs[d] {
			orphanLayerDirs = append(orphanLayerDirs, d)
		}
	}

	// PG rows without on-disk directory.
	diskLayerSet := make(map[string]bool)
	for _, d := range layersDirs {
		diskLayerSet[d] = true
	}
	var pgOrphanLayers []string
	for _, l := range allLayers {
		if !diskLayerSet[l.LayerID] && l.LayerID != "" {
			pgOrphanLayers = append(pgOrphanLayers, l.LayerID)
		}
	}

	// Uppers PVC: directories on disk that aren't tracked in any session.
	var orphanUpperDirs []string
	for _, d := range uppersDirs {
		if d == "" {
			continue
		}
		if !knownSessionIDs[d] {
			orphanUpperDirs = append(orphanUpperDirs, d)
		}
	}

	// --- Phase 4: Print report ---
	fmt.Println()
	fmt.Println("Layers PVC (/mnt/astonish-layers):")
	fmt.Printf("  Total directories on disk:   %d\n", len(layersDirs))
	fmt.Printf("  Tracked in PG:               %d\n", len(allLayers))
	fmt.Printf("  Orphan directories (disk, no PG row):  %d\n", len(orphanLayerDirs))
	fmt.Printf("  Staging dirs (__staging-*):   %d\n", len(stagingDirs))
	fmt.Printf("  PG rows with ref_count=0 (>%s old):  %d\n", grace, len(unreferencedLayers))
	fmt.Printf("  PG rows without disk dir:    %d\n", len(pgOrphanLayers))
	fmt.Println()
	fmt.Println("Uppers PVC (/mnt/astonish-uppers):")
	fmt.Printf("  Total session directories:   %d\n", len(uppersDirs))
	fmt.Printf("  Tracked in PG sessions:      %d\n", len(knownSessionIDs))
	fmt.Printf("  Orphan directories:          %d\n", len(orphanUpperDirs))
	fmt.Println()

	totalOrphans := len(orphanLayerDirs) + len(stagingDirs) + len(orphanUpperDirs)
	if totalOrphans == 0 && len(pgOrphanLayers) == 0 && len(unreferencedLayers) == 0 {
		fmt.Println("No orphans detected. Storage is clean.")
		return nil
	}

	if len(orphanLayerDirs) > 0 {
		fmt.Println("Orphan layer directories (no PG row):")
		for _, d := range orphanLayerDirs {
			fmt.Printf("  - %s\n", d)
		}
		fmt.Println()
	}
	if len(stagingDirs) > 0 {
		fmt.Println("Orphan staging directories (crashed builders):")
		for _, d := range stagingDirs {
			fmt.Printf("  - %s\n", d)
		}
		fmt.Println()
	}
	if len(orphanUpperDirs) > 0 {
		fmt.Println("Orphan upper directories (no active session):")
		for _, d := range orphanUpperDirs {
			fmt.Printf("  - %s\n", d)
		}
		fmt.Println()
	}
	if len(unreferencedLayers) > 0 {
		fmt.Println("PG layers with ref_count=0 (reclaimable):")
		for _, l := range unreferencedLayers {
			fmt.Printf("  - %s (added %s)\n", l.LayerID, l.AddedAt.Format(time.RFC3339))
		}
		fmt.Println()
	}
	if len(pgOrphanLayers) > 0 {
		fmt.Println("PG layer rows without disk directory (data already gone):")
		for _, id := range pgOrphanLayers {
			fmt.Printf("  - %s\n", id)
		}
		fmt.Println()
	}

	if !reclaim && !reclaimPG {
		fmt.Println("Re-run with --reclaim to delete orphan directories from PVCs.")
		fmt.Println("Re-run with --reclaim-pg to also remove orphan PG rows.")
		return nil
	}

	// --- Phase 5: Reclaim ---
	if reclaim {
		toDelete := append(orphanLayerDirs, stagingDirs...)
		if len(toDelete) > 0 {
			fmt.Printf("\nReclaiming %d orphan director(ies) from layers PVC...\n", len(toDelete))
			if err := reclaimDirsFromPVC(ctx, cs, namespace, appCfg, "layers", toDelete); err != nil {
				fmt.Printf("  WARNING: layer reclaim failed: %v\n", err)
			} else {
				fmt.Printf("  Done. Removed %d director(ies).\n", len(toDelete))
			}
		}

		// Reclaim unreferenced layers (ref_count=0).
		if len(unreferencedLayers) > 0 {
			unreferencedDirs := make([]string, 0, len(unreferencedLayers))
			for _, l := range unreferencedLayers {
				unreferencedDirs = append(unreferencedDirs, l.LayerID)
			}
			fmt.Printf("\nReclaiming %d unreferenced layer director(ies)...\n", len(unreferencedDirs))
			if err := reclaimDirsFromPVC(ctx, cs, namespace, appCfg, "layers", unreferencedDirs); err != nil {
				fmt.Printf("  WARNING: layer reclaim failed: %v\n", err)
			} else {
				fmt.Printf("  Done. Removed %d director(ies) from disk.\n", len(unreferencedDirs))
				// Also delete PG rows for reclaimed layers.
				for _, l := range unreferencedLayers {
					if err := layers.DeleteLayer(ctx, l.LayerID); err != nil {
						fmt.Printf("  WARNING: failed to delete PG row for %s: %v\n", l.LayerID, err)
					}
				}
				fmt.Printf("  Removed %d PG rows.\n", len(unreferencedLayers))
			}
		}

		if len(orphanUpperDirs) > 0 {
			fmt.Printf("\nReclaiming %d orphan director(ies) from uppers PVC...\n", len(orphanUpperDirs))
			if err := reclaimDirsFromPVC(ctx, cs, namespace, appCfg, "uppers", orphanUpperDirs); err != nil {
				fmt.Printf("  WARNING: uppers reclaim failed: %v\n", err)
			} else {
				fmt.Printf("  Done. Removed %d director(ies).\n", len(orphanUpperDirs))
			}
		}
	}

	if reclaimPG && len(pgOrphanLayers) > 0 {
		fmt.Printf("\nRemoving %d PG layer row(s) that have no on-disk directory...\n", len(pgOrphanLayers))
		removed := 0
		for _, id := range pgOrphanLayers {
			if err := layers.DeleteLayer(ctx, id); err != nil {
				fmt.Printf("  WARNING: failed to delete PG row %s: %v\n", id, err)
			} else {
				removed++
			}
		}
		fmt.Printf("  Removed %d PG row(s).\n", removed)
	}

	fmt.Println("\nAudit complete.")
	return nil
}

type pgLayerInfo struct {
	LayerID   string
	SizeBytes int64
	AddedAt   time.Time
}

// runAuditPod spawns a pod that mounts both PVCs and prints directory listings
// as JSON. Returns the list of top-level directory names for each PVC.
func runAuditPod(ctx context.Context, cs kubernetes.Interface, namespace string, appCfg *config.AppConfig) ([]string, []string, error) {
	layersPVC := "astonish-layers"
	uppersPVC := "astonish-uppers"
	if appCfg.Sandbox.Kubernetes.LayersPVCName != "" {
		layersPVC = appCfg.Sandbox.Kubernetes.LayersPVCName
	}
	if appCfg.Sandbox.Kubernetes.UppersPVCName != "" {
		uppersPVC = appCfg.Sandbox.Kubernetes.UppersPVCName
	}

	podName := fmt.Sprintf("astn-audit-%d", time.Now().Unix())

	// The script outputs JSON: {"layers": [...], "uppers": [...]}
	script := `#!/bin/sh
set -e
LAYERS=$(ls -1 /mnt/layers 2>/dev/null || echo "")
UPPERS=$(ls -1 /mnt/uppers 2>/dev/null || echo "")
printf '{"layers":%s,"uppers":%s}\n' \
  "$(echo "$LAYERS" | awk 'BEGIN{printf "["} NR>1{printf ","} {gsub(/"/,"\\\""); printf "\"%s\"",$0} END{printf "]"}')" \
  "$(echo "$UPPERS" | awk 'BEGIN{printf "["} NR>1{printf ","} {gsub(/"/,"\\\""); printf "\"%s\"",$0} END{printf "]"}')"
`

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"astonish.io/type": "audit",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "audit",
					Image:           "alpine:3.21",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/bin/sh", "-c", script},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "layers", MountPath: "/mnt/layers", ReadOnly: true},
						{Name: "uppers", MountPath: "/mnt/uppers", ReadOnly: true},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "layers",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: layersPVC,
							ReadOnly:  true,
						},
					},
				},
				{
					Name: "uppers",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: uppersPVC,
							ReadOnly:  true,
						},
					},
				},
			},
		},
	}

	// Cleanup pod on exit.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cs.CoreV1().Pods(namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{})
	}()

	// Delete stale pod if one exists from a previous run.
	_ = cs.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	time.Sleep(500 * time.Millisecond)

	if _, err := cs.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return nil, nil, fmt.Errorf("create audit pod: %w", err)
	}

	// Wait for completion.
	if err := waitForPodComplete(ctx, cs, namespace, podName, 120*time.Second); err != nil {
		return nil, nil, err
	}

	// Read logs.
	logReq := cs.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{})
	logStream, err := logReq.Stream(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read audit pod logs: %w", err)
	}
	defer logStream.Close()

	var buf strings.Builder
	b := make([]byte, 4096)
	for {
		n, readErr := logStream.Read(b)
		if n > 0 {
			buf.Write(b[:n])
		}
		if readErr != nil {
			break
		}
	}

	var result struct {
		Layers []string `json:"layers"`
		Uppers []string `json:"uppers"`
	}
	if err := json.Unmarshal([]byte(buf.String()), &result); err != nil {
		return nil, nil, fmt.Errorf("parse audit pod output: %w (raw: %q)", err, buf.String())
	}

	// Filter empty strings from the listing.
	result.Layers = filterEmpty(result.Layers)
	result.Uppers = filterEmpty(result.Uppers)

	fmt.Printf("  Layers: %d directories, Uppers: %d directories\n", len(result.Layers), len(result.Uppers))
	return result.Layers, result.Uppers, nil
}

// reclaimDirsFromPVC spawns a pod that mounts the specified PVC and removes
// the given directories.
func reclaimDirsFromPVC(ctx context.Context, cs kubernetes.Interface, namespace string, appCfg *config.AppConfig, pvcType string, dirs []string) error {
	var pvcName, mountPath string
	switch pvcType {
	case "layers":
		pvcName = "astonish-layers"
		mountPath = "/mnt/layers"
		if appCfg.Sandbox.Kubernetes.LayersPVCName != "" {
			pvcName = appCfg.Sandbox.Kubernetes.LayersPVCName
		}
	case "uppers":
		pvcName = "astonish-uppers"
		mountPath = "/mnt/uppers"
		if appCfg.Sandbox.Kubernetes.UppersPVCName != "" {
			pvcName = appCfg.Sandbox.Kubernetes.UppersPVCName
		}
	default:
		return fmt.Errorf("unknown PVC type: %s", pvcType)
	}

	// Build rm commands. Batch into one script for efficiency.
	var sb strings.Builder
	sb.WriteString("#!/bin/sh\nset -e\n")
	for _, d := range dirs {
		// Safety: reject any path traversal.
		if strings.Contains(d, "/") || strings.Contains(d, "..") || d == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf("rm -rf '%s/%s'\n", mountPath, d))
	}

	podName := fmt.Sprintf("astn-gc-%s-%d", pvcType, time.Now().Unix())

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: namespace,
			Labels: map[string]string{
				"astonish.io/type": "audit-gc",
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:            "gc",
					Image:           "alpine:3.21",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"/bin/sh", "-c", sb.String()},
					VolumeMounts: []corev1.VolumeMount{
						{Name: "data", MountPath: mountPath},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "data",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}

	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = cs.CoreV1().Pods(namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{})
	}()

	_ = cs.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{})
	time.Sleep(500 * time.Millisecond)

	if _, err := cs.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("create gc pod: %w", err)
	}

	return waitForPodComplete(ctx, cs, namespace, podName, 120*time.Second)
}

// waitForPodComplete polls until the pod reaches Succeeded or Failed.
func waitForPodComplete(ctx context.Context, cs kubernetes.Interface, namespace, podName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("pod %s did not complete within %s", podName, timeout)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		pod, err := cs.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return fmt.Errorf("pod %s disappeared", podName)
			}
			return err
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return nil
		case corev1.PodFailed:
			return fmt.Errorf("pod %s failed", podName)
		}
		time.Sleep(2 * time.Second)
	}
}

// getAllSessionIDs queries all team schemas for session IDs.
func getAllSessionIDs(ctx context.Context, pgStore *pgstore.PGStore) (map[string]bool, error) {
	schemas, err := pgStore.ListTeamSchemas(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]bool)
	for _, schema := range schemas {
		sessStore := pgStore.SandboxSessionsForSchema(schema)
		if sessStore == nil {
			continue
		}
		sessions, err := sessStore.List(ctx, store.SandboxSessionFilter{})
		if err != nil {
			// Non-fatal: some schemas might not have the sessions table yet.
			continue
		}
		for _, s := range sessions {
			result[s.SessionID] = true
		}
	}
	return result, nil
}

func filterEmpty(ss []string) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func printSandboxAuditUsage() {
	fmt.Println("usage: astonish platform sandbox-audit [options]")
	fmt.Println("")
	fmt.Println("Audit sandbox PVCs for orphaned data and optionally reclaim space.")
	fmt.Println("")
	fmt.Println("This command spawns a read-only pod in the sandbox namespace to list")
	fmt.Println("directories on both PVCs, then diffs against PostgreSQL to find orphans.")
	fmt.Println("Works from any machine with kubectl access (uses your kubeconfig).")
	fmt.Println("")
	fmt.Println("options:")
	fmt.Println("  --reclaim         Delete orphan directories from PVCs")
	fmt.Println("  --reclaim-pg      Also remove PG layer rows that have no on-disk directory")
	fmt.Println("  --namespace NS    Override sandbox namespace (default: from config)")
	fmt.Println("  --kubeconfig PATH Override kubeconfig path")
	fmt.Println("  --grace DURATION  Grace period for orphan detection (default: 1h)")
	fmt.Println("")
	fmt.Println("examples:")
	fmt.Println("  astonish platform sandbox-audit")
	fmt.Println("  astonish platform sandbox-audit --reclaim --grace 24h")
	fmt.Println("  astonish platform sandbox-audit --reclaim --reclaim-pg")
	fmt.Println("  astonish platform sandbox-audit --kubeconfig ~/.kube/prod --namespace astonish-sandbox")
}
