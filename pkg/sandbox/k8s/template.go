// Package k8s — template operations.
//
// This file implements BuildTemplate and SaveSessionAsTemplate against a
// kubernetes.Interface client and the SPDY exec transport. The shared
// mechanics are:
//
//  1. Identify a pod in which to run the in-pod tar-to-layer pipeline.
//     BuildTemplate spawns a short-lived template-builder pod whose
//     upper emptyDir is the source of the new layer. SaveSessionAsTemplate
//     re-uses the target session's pod and streams its upper dir.
//
//  2. Exec the canonical pipeline (§5.11 — `tar --numeric-owner --xattrs
//     --acls --sort=name --mtime=@0` teed through sha256sum into an
//     in-pod extract of the staging directory on the RW CephFS mount).
//     The pipeline runs with `set -e` so any stage failure surfaces as a
//     non-zero exit code.
//
//  3. Parse SHA= and SIZE= lines from the pipeline's stdout to build
//     the returned sandbox.TemplateArtifact.
//
//  4. Clean up: BuildTemplate destroys the builder pod unconditionally
//     (success or failure) once its output has been consumed.
//     SaveSessionAsTemplate leaves the session intact.
//
// This slice intentionally does NOT:
//   - Insert into SandboxTemplateStore or LayerStore. That wiring is the
//     caller's responsibility (§3.3: "the template DAG metadata is owned
//     by SandboxTemplateStore (Phase A) and wired by calling code").
//   - Wait for the builder pod to be Ready. Once a later slice lands an
//     overlay-entrypoint that bootstraps the merged view, BuildTemplate
//     will gate the first exec on a Pod-phase=Running + Ready=True
//     condition. For now the exec itself will block until the kubelet
//     attaches, which is adequate for the contract.
//   - Implement RefreshTemplate. Refresh requires the build recipe stored
//     in sandbox_templates.build_spec; that column is not yet modelled in
//     store.SandboxTemplate. Until it is, RefreshTemplate returns a clear
//     "requires build-spec persistence" error.
//   - Implement synchronous byte deletion in DeleteTemplate. On K8s the
//     layer bytes are reclaimed by the GC reconciler (§5.12) once the
//     caller has decremented ref_count. DeleteTemplate is therefore a
//     no-op at the backend tier — it returns nil so callers can invoke
//     it uniformly without backend-kind branching.
//
// Reference: docs/architecture/sandbox-backends.md §5.6 (templates),
// §5.11 (layer store), §5.12 (GC).

package k8s

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/schardosin/astonish/pkg/sandbox"
)

// builderPodNamePrefix is the stable prefix for template-builder pods.
// Operators grepping `kubectl get pods` can pick them out by this prefix.
const builderPodNamePrefix = "astn-tmpl-"

// builderPodReadinessTimeout caps how long BuildTemplate waits for the
// builder pod to become Running before issuing its first exec. The
// exec itself will block until the kubelet has attached, so this
// timeout primarily protects against ImagePullBackOff / scheduling
// hangs turning into silent stalls.
const builderPodReadinessTimeout = 2 * time.Minute

// builderPodReadinessPollInterval is the poll cadence for the readiness
// wait. Chosen small enough that fast-starting pods (~seconds) don't
// add perceptible latency.
const builderPodReadinessPollInterval = 500 * time.Millisecond

// ---------------------------------------------------------------------------
// BuildTemplate
// ---------------------------------------------------------------------------

// BuildTemplate provisions a short-lived template-builder pod composed
// on top of spec.ParentLayers, execs the supplied build steps, captures
// the resulting upper layer as a new content-addressed layer on CephFS,
// and returns its artifact. The builder pod is deleted before return in
// all paths (success, step failure, pipeline failure).
//
// See docs/architecture/sandbox-backends.md §5.6 "CreateTemplate".
func (b *K8sBackend) BuildTemplate(ctx context.Context, spec sandbox.TemplateBuildSpec) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.client == nil {
		return nil, fmt.Errorf("BuildTemplate: %w", ErrNotImplementedYet)
	}
	if spec.TemplateID == "" {
		return nil, errors.New("sandbox/k8s: BuildTemplate: TemplateID is required")
	}
	if len(spec.ParentLayers) > b.cfg.MaxChainDepth {
		return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: parent layer chain depth %d exceeds MaxChainDepth %d",
			len(spec.ParentLayers), b.cfg.MaxChainDepth)
	}

	pod, err := b.buildTemplateBuilderPodManifest(spec)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: build manifest: %w", err)
	}

	created, err := b.client.CoreV1().Pods(b.cfg.Namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: create builder pod: %w", err)
	}
	if apierrors.IsAlreadyExists(err) {
		// Unlikely (the builder name contains a timestamp) but not
		// impossible; fetch so we can observe the actual phase before
		// deciding to tear down.
		created, err = b.client.CoreV1().Pods(b.cfg.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: fetch existing builder pod: %w", err)
		}
	}
	podName := created.Name

	// Always attempt to delete the builder pod, even on error paths.
	// We use a best-effort background context so a cancelled outer
	// ctx still tears the pod down.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = b.client.CoreV1().Pods(b.cfg.Namespace).Delete(cleanupCtx, podName, metav1.DeleteOptions{})
	}()

	if err := b.waitForBuilderPodRunning(ctx, podName); err != nil {
		return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: wait pod running: %w", err)
	}

	// Run build steps sequentially. Each step is a single /bin/sh -c
	// invocation. Non-zero exits abort the build; stderr is surfaced
	// in the returned error for operator diagnostics.
	for i, step := range spec.Steps {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		res, err := b.execInPod(ctx, podName, sandbox.ExecSpec{
			Command: []string{"/bin/sh", "-c", step},
		})
		if err != nil {
			return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: step %d (%q): %w", i, truncate(step, 120), err)
		}
		if res.ExitCode != 0 {
			return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: step %d (%q) exited %d: stderr=%s",
				i, truncate(step, 120), res.ExitCode, truncate(string(res.Stderr), 512))
		}
	}

	artifact, err := b.captureUpperAsLayer(ctx, podName, parentLayerOf(spec.ParentLayers))
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: BuildTemplate: capture upper: %w", err)
	}
	return artifact, nil
}

// ---------------------------------------------------------------------------
// SaveSessionAsTemplate
// ---------------------------------------------------------------------------

// SaveSessionAsTemplate captures the upper layer of a running session
// as a new immutable content-addressed layer. The session pod remains
// running. Fast: 1-5 seconds for typical deltas (§5.6 "SaveSessionAsTemplate").
//
// Parent-layer tracking: the returned artifact's ParentLayer is the
// TemplateID of the running session. Callers that maintain the DAG
// (store.SandboxTemplateStore) are responsible for resolving that
// TemplateID to a concrete layer_id when they insert the new template
// row; the backend tier does not know the template → top_layer_id
// mapping.
func (b *K8sBackend) SaveSessionAsTemplate(ctx context.Context, sessionID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.client == nil {
		return nil, fmt.Errorf("SaveSessionAsTemplate: %w", ErrNotImplementedYet)
	}
	if sessionID == "" {
		return nil, errors.New("sandbox/k8s: SaveSessionAsTemplate: sessionID is required")
	}

	rec, err := b.sessions.GetSession(sessionID)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: SaveSessionAsTemplate(%s): lookup session: %w", sessionID, err)
	}
	if rec == nil || rec.PodName == "" {
		return nil, fmt.Errorf("sandbox/k8s: SaveSessionAsTemplate: session %q has no pod", sessionID)
	}

	// ParentLayer is TemplateID here; see doc-comment rationale.
	artifact, err := b.captureUpperAsLayer(ctx, rec.PodName, rec.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("sandbox/k8s: SaveSessionAsTemplate(%s): %w", sessionID, err)
	}
	return artifact, nil
}

// ---------------------------------------------------------------------------
// RefreshTemplate / DeleteTemplate
// ---------------------------------------------------------------------------

// RefreshTemplate re-runs a template's recorded build steps on top of
// its current parent chain, producing a new top layer (§5.6
// "RefreshTemplate").
//
// Refresh requires access to the template's build recipe, which the
// spec stores in sandbox_templates.build_spec JSONB. That column is not
// yet modelled in store.SandboxTemplate; until it is, RefreshTemplate
// has nothing to execute and returns a descriptive error. Callers that
// need this functionality today must call BuildTemplate with the spec
// they wish to apply.
func (b *K8sBackend) RefreshTemplate(ctx context.Context, templateID string) (*sandbox.TemplateArtifact, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if b.client == nil {
		return nil, fmt.Errorf("RefreshTemplate: %w", ErrNotImplementedYet)
	}
	return nil, errors.New("sandbox/k8s: RefreshTemplate: requires build-spec persistence on store.SandboxTemplate (not yet implemented); call BuildTemplate with the desired spec")
}

// DeleteTemplate is a no-op on the K8s backend.
//
// Rationale: on K8s, template layer bytes live on CephFS as
// content-addressed directories. Their lifetime is governed by
// reference counts in platform.sandbox_layers (maintained transactionally
// by the caller) and reclaimed asynchronously by the GC reconciler
// (§5.12). There is no backend-tier synchronous byte-deletion operation.
//
// The force flag is ignored for the same reason — the backend has no
// handle on "does any session or child template still reference this?"
// That check lives in the caller, which holds the platform-scoped DB.
func (b *K8sBackend) DeleteTemplate(ctx context.Context, templateID string, force bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// Intentional no-op. See doc comment above.
	_ = templateID
	_ = force
	return nil
}

// ---------------------------------------------------------------------------
// Builder pod manifest
// ---------------------------------------------------------------------------

// buildTemplateBuilderPodManifest materialises a template-builder Pod
// spec from a TemplateBuildSpec. The resulting pod shares session pods'
// overall shape (same image, same volumes, same RuntimeClass) with two
// differences:
//
//   - The layers PVC is mounted READ-WRITE so the in-pod tar pipeline
//     can atomic-rename the staging directory into place.
//   - Labels advertise astonish.io/type=template-builder so NetworkPolicy
//     selectors and operator queries can distinguish builders from
//     sessions.
//
// The container runs `sleep infinity` so it stays up until an exec
// attaches; BuildTemplate will execute build steps via the exec
// transport, then tear the pod down.
//
// Exposed at package scope (not a method on K8sBackend) to be
// unit-testable without a live client.
func (b *K8sBackend) buildTemplateBuilderPodManifest(spec sandbox.TemplateBuildSpec) (*corev1.Pod, error) {
	if spec.TemplateID == "" {
		return nil, errors.New("TemplateBuildSpec.TemplateID is required")
	}
	// Include the template ID plus a timestamp suffix so repeated builds of
	// the same template don't collide on a retained name if the defer-delete
	// is still in flight.
	suffix := time.Now().UTC().Format("20060102150405")
	name := builderPodNamePrefix + toDNSLabel(spec.TemplateID)
	// Cap the base portion of the name so the combined length stays within
	// the 253-char DNS-1123 limit (realistic inputs are far below this).
	const maxBase = 200
	if len(name) > maxBase {
		name = name[:maxBase]
		for len(name) > 0 && name[len(name)-1] == '-' {
			name = name[:len(name)-1]
		}
	}
	name = name + "-" + suffix

	labels := map[string]string{
		labelType:     typeTemplateBuilder,
		labelTemplate: toDNSLabel(spec.TemplateID),
	}
	for k, v := range spec.Labels {
		labels[k] = v
	}

	annotations := map[string]string{
		annotationCreatedAt:  time.Now().UTC().Format(time.RFC3339),
		annotationLayerChain: strings.Join(spec.ParentLayers, ","),
	}

	runtimeClass := b.cfg.RuntimeClassName
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   b.cfg.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			RuntimeClassName: &runtimeClass,
			RestartPolicy:    corev1.RestartPolicyNever,
			Containers: []corev1.Container{
				{
					Name:    containerName,
					Image:   b.cfg.SandboxImage,
					Command: []string{"/bin/sh", "-c", "sleep infinity"},
					Env: []corev1.EnvVar{
						{Name: "ASTONISH_TEMPLATE_ID", Value: spec.TemplateID},
						{Name: "ASTONISH_LAYER_CHAIN", Value: strings.Join(spec.ParentLayers, ",")},
						{Name: "ASTONISH_UPPER_DIR", Value: mountUpper},
						{Name: "ASTONISH_WORK_DIR", Value: mountWork},
						{Name: "ASTONISH_LAYERS_DIR", Value: mountLayers},
					},
					VolumeMounts: []corev1.VolumeMount{
						{Name: volumeLayers, MountPath: mountLayers}, // RW for atomic rename
						{Name: volumeUpper, MountPath: mountUpper},
						{Name: volumeWork, MountPath: mountWork},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: volumeLayers,
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: b.cfg.LayersPVCName,
						},
					},
				},
				{Name: volumeUpper, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: volumeWork, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
		},
	}

	return pod, nil
}

// ---------------------------------------------------------------------------
// Shared capture pipeline
// ---------------------------------------------------------------------------

// captureUpperAsLayer execs the canonical tar-to-layer pipeline in the
// given pod and returns the resulting TemplateArtifact.
//
// The pipeline:
//   1. tar's /var/astonish/upper with fixed canonical options (see §5.11).
//   2. Tees the stream through sha256sum into a staging directory on
//      /mnt/astonish-layers.
//   3. Atomic-renames staging → /mnt/astonish-layers/<sha>/. If a
//      directory with that sha already exists (content dedup via
//      idempotent sha256), the staging copy is removed.
//   4. Computes the on-disk size of the final rootfs directory.
//   5. Emits SHA=<hex>\nSIZE=<bytes>\n on stdout for us to parse.
//
// parentLayer is embedded in the returned artifact's ParentLayer field
// (nil / empty string is fine — root layers have no parent). The
// backend tier does no independent validation of the parent link; that's
// the template store's responsibility.
func (b *K8sBackend) captureUpperAsLayer(ctx context.Context, podName, parentLayer string) (*sandbox.TemplateArtifact, error) {
	builderID := fmt.Sprintf("%s-%d", podName, time.Now().UTC().UnixNano())
	script := buildCaptureScript(b.cfg.LayersPath, builderID)

	res, err := b.execInPod(ctx, podName, sandbox.ExecSpec{
		Command: []string{"/bin/sh", "-c", script},
	})
	if err != nil {
		return nil, fmt.Errorf("exec capture pipeline: %w", err)
	}
	if res.ExitCode != 0 {
		return nil, fmt.Errorf("capture pipeline exited %d: stderr=%s",
			res.ExitCode, truncate(string(res.Stderr), 512))
	}

	sha, size, err := parseCaptureOutput(res.Stdout)
	if err != nil {
		return nil, fmt.Errorf("parse capture output: %w (stdout=%q)", err, truncate(string(res.Stdout), 512))
	}

	return &sandbox.TemplateArtifact{
		LayerID:     sha,
		ParentLayer: parentLayer,
		SizeBytes:   size,
		CephFSPath:  fmt.Sprintf("%s/%s/rootfs", b.cfg.LayersPath, sha),
		CreatedAt:   time.Now().UTC(),
	}, nil
}

// buildCaptureScript emits the shell pipeline (see captureUpperAsLayer
// for the sequencing). layersPath is the in-pod mount point for the
// RW layer store (typically /mnt/astonish-layers). builderID is a
// short unique token embedded in the staging path to avoid collisions
// between concurrent captures on the same CephFS directory.
//
// Isolated at package scope so tests can assert exactly the script
// shape without standing up a pod.
func buildCaptureScript(layersPath, builderID string) string {
	var b bytes.Buffer
	b.WriteString("set -e\n")
	fmt.Fprintf(&b, "STAGING=%q\n", layersPath+"/__staging-"+builderID)
	fmt.Fprintf(&b, "LAYERS_DIR=%q\n", layersPath)
	b.WriteString("mkdir -p \"$STAGING/rootfs\"\n")
	// Canonical tar: --sort=name --mtime=@0 pins layout byte-for-byte
	// across runs with identical content. --numeric-owner --xattrs
	// --acls match the preservation requirements (§5.6).
	b.WriteString("tar --numeric-owner --xattrs --acls --sort=name --mtime=@0 \\\n")
	b.WriteString("    -C /var/astonish/upper -cf - . \\\n")
	b.WriteString("  | tee >(sha256sum > /tmp/astn-sha) \\\n")
	b.WriteString("  | tar --numeric-owner --xattrs --acls -C \"$STAGING/rootfs\" -xf -\n")
	b.WriteString("SHA=$(awk '{print $1}' /tmp/astn-sha)\n")
	b.WriteString("if [ -d \"$LAYERS_DIR/$SHA\" ]; then\n")
	b.WriteString("  rm -rf \"$STAGING\"\n")
	b.WriteString("else\n")
	b.WriteString("  mv \"$STAGING\" \"$LAYERS_DIR/$SHA\"\n")
	b.WriteString("fi\n")
	b.WriteString("SIZE=$(du -sb \"$LAYERS_DIR/$SHA/rootfs\" | awk '{print $1}')\n")
	b.WriteString("echo \"SHA=$SHA\"\n")
	b.WriteString("echo \"SIZE=$SIZE\"\n")
	return b.String()
}

// parseCaptureOutput extracts the SHA= and SIZE= lines emitted by
// buildCaptureScript. Both must be present and well-formed; anything
// else is a protocol error. Extra lines are ignored so the script can
// be augmented with diagnostics without breaking this parser.
func parseCaptureOutput(stdout []byte) (sha string, size int64, err error) {
	sc := bufio.NewScanner(bytes.NewReader(stdout))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "SHA="):
			sha = strings.TrimPrefix(line, "SHA=")
		case strings.HasPrefix(line, "SIZE="):
			v := strings.TrimPrefix(line, "SIZE=")
			n, perr := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if perr != nil {
				return "", 0, fmt.Errorf("SIZE= is not an integer: %w", perr)
			}
			size = n
		}
	}
	if sha == "" {
		return "", 0, errors.New("SHA= line missing")
	}
	if len(sha) != 64 {
		return "", 0, fmt.Errorf("SHA= value %q is not 64-char hex", sha)
	}
	if size < 0 {
		return "", 0, fmt.Errorf("SIZE= %d is negative", size)
	}
	return sha, size, nil
}

// ---------------------------------------------------------------------------
// Readiness wait
// ---------------------------------------------------------------------------

// waitForBuilderPodRunning polls the builder pod's phase until it is
// Running (or past, e.g. Succeeded/Failed, which we treat as terminal
// failure since the builder is sleep infinity and must remain Running).
// Honours both the supplied ctx and builderPodReadinessTimeout.
func (b *K8sBackend) waitForBuilderPodRunning(ctx context.Context, podName string) error {
	deadline := time.Now().Add(builderPodReadinessTimeout)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		pod, err := b.client.CoreV1().Pods(b.cfg.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("get builder pod: %w", err)
		}
		switch pod.Status.Phase {
		case corev1.PodRunning:
			return nil
		case corev1.PodSucceeded, corev1.PodFailed:
			return fmt.Errorf("builder pod reached terminal phase %s before Running", pod.Status.Phase)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("builder pod did not become Running within %s (phase=%s)",
				builderPodReadinessTimeout, pod.Status.Phase)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(builderPodReadinessPollInterval):
		}
	}
}

// ---------------------------------------------------------------------------
// Small helpers
// ---------------------------------------------------------------------------

// parentLayerOf returns the last (top-most) entry of a layer chain, or
// "" if the chain is empty (root layer case).
func parentLayerOf(chain []string) string {
	if len(chain) == 0 {
		return ""
	}
	return chain[len(chain)-1]
}

// truncate returns s clipped to at most n bytes, with "…" appended if
// it actually clipped. Used exclusively for error-message readability.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
