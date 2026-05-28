//go:build e2e

package e2eboot

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const defaultSandboxNamespace = "astonish-sandbox"

// SandboxNamespace returns the K8s namespace for sandbox pods. It honors
// ASTONISH_E2E_SANDBOX_NAMESPACE so tests stay aligned with whatever
// e2eboot.Bootstrap wrote into the server's config.yaml.
func SandboxNamespace() string {
	if ns := os.Getenv("ASTONISH_E2E_SANDBOX_NAMESPACE"); ns != "" {
		return ns
	}
	return defaultSandboxNamespace
}

// DerivePodName converts a session ID into the expected K8s pod name.
func DerivePodName(sessionID string) string {
	const prefix = "astn-sess-"
	const maxIDLen = 27
	clean := strings.ToLower(sessionID)
	var out []byte
	for i := range clean {
		c := clean[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, c)
		} else {
			out = append(out, '-')
		}
	}
	s := strings.Trim(string(out), "-")
	if len(s) > maxIDLen {
		s = s[:maxIDLen]
	}
	s = strings.TrimRight(s, "-")
	return prefix + s
}

// WaitForPodRunning polls kubectl until the named pod reaches Running phase.
func WaitForPodRunning(t *testing.T, podName string, timeout time.Duration) {
	t.Helper()
	ns := SandboxNamespace()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("kubectl", "get", "pod", "-n", ns, podName,
			"-o", "jsonpath={.status.phase}").Output()
		if err == nil && strings.TrimSpace(string(out)) == "Running" {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("[e2eboot] pod %s did not reach Running within %v", podName, timeout)
}

// GetPodAnnotation retrieves a specific annotation value from a pod.
func GetPodAnnotation(t *testing.T, podName, annotation string) string {
	t.Helper()
	ns := SandboxNamespace()
	jsonPath := fmt.Sprintf(`{.metadata.annotations.%s}`, annotation)
	out, err := exec.Command("kubectl", "get", "pod", "-n", ns, podName,
		"-o", fmt.Sprintf("jsonpath=%s", jsonPath)).Output()
	if err != nil {
		t.Fatalf("[e2eboot] get pod annotation %s on %s: %v", annotation, podName, err)
	}
	return strings.TrimSpace(string(out))
}

// GetPodChain retrieves the layer-chain annotation from a sandbox pod.
func GetPodChain(t *testing.T, podName string) string {
	t.Helper()
	return GetPodAnnotation(t, podName, `astonish\.io/layer-chain`)
}

// AssertCommandPresent verifies a command is available in the sandbox rootfs.
func AssertCommandPresent(t *testing.T, podName, cmd, msg string) {
	t.Helper()
	ns := SandboxNamespace()
	err := exec.Command("kubectl", "exec", "-n", ns, podName, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c", "command -v "+cmd).Run()
	if err != nil {
		t.Errorf("FAIL: %s (command '%s' not found in pod %s)", msg, cmd, podName)
	} else {
		t.Logf("  OK: %s", msg)
	}
}

// AssertCommandAbsent verifies a command is NOT available in the sandbox rootfs.
func AssertCommandAbsent(t *testing.T, podName, cmd, msg string) {
	t.Helper()
	ns := SandboxNamespace()
	err := exec.Command("kubectl", "exec", "-n", ns, podName, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c", "command -v "+cmd).Run()
	if err == nil {
		t.Errorf("FAIL: %s (command '%s' IS present in pod %s but should not be)", msg, cmd, podName)
	} else {
		t.Logf("  OK: %s", msg)
	}
}

// DeletePod force-deletes a pod and waits for it to disappear.
func DeletePod(t *testing.T, podName string) {
	t.Helper()
	ns := SandboxNamespace()
	_ = exec.Command("kubectl", "delete", "pod", "-n", ns, podName,
		"--grace-period=5", "--ignore-not-found").Run()

	for i := 0; i < 10; i++ {
		out, err := exec.Command("kubectl", "get", "pod", "-n", ns, podName,
			"-o", "jsonpath={.status.phase}").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			return
		}
		time.Sleep(2 * time.Second)
	}
}

// SweepAllPods deletes all pods in the sandbox namespace. This is used as
// both a pre-test cleanup (remove stale pods from a previously crashed run)
// and a post-test cleanup (remove pods created during the test). The
// namespace and PVCs are preserved so the next test run doesn't need to
// re-provision infrastructure or rebuild the base layer.
func SweepAllPods(t *testing.T) {
	t.Helper()
	ns := SandboxNamespace()
	out, err := exec.Command("kubectl", "delete", "pods", "--all", "-n", ns,
		"--grace-period=5", "--ignore-not-found").CombinedOutput()
	if err != nil {
		t.Logf("[e2eboot] SweepAllPods: %v (output: %s)", err, strings.TrimSpace(string(out)))
	} else {
		t.Logf("[e2eboot] SweepAllPods: cleaned sandbox namespace %s", ns)
	}

	// Sweep orphan staging directories left by crashed template builder pods.
	// This is critical to prevent unbounded growth on the layers PVC.
	// Note: We deliberately do NOT sweep layer directories here. Both the raw
	// @base rootfs and the "Configure Base" platform layer are created during
	// `make e2e-k8s-up` and are meant to survive across multiple test runs
	// within the same infra provisioning. Sweeping them would force every test
	// that needs a sandbox to re-run the expensive base configuration build.
	SweepStagingDirs(t)
}

// SweepStagingDirs removes __staging-* directories from the astonish-layers PVC.
// These are left behind when template builder pods are killed (OOM, node pressure, timeout).
// We spawn a short-lived alpine pod that mounts the PVC and does the rm.
func SweepStagingDirs(t *testing.T) {
	t.Helper()
	ns := SandboxNamespace()
	podName := fmt.Sprintf("sweep-staging-%d", time.Now().UnixNano()%100000000)

	// Use a one-shot pod that mounts the layers PVC directly.
	overrides := `{"spec":{"restartPolicy":"Never","containers":[{"name":"sweep","image":"alpine:3.21","command":["sh","-c","rm -rf /layers/__staging-* 2>/dev/null; echo 'staging cleaned'"],"volumeMounts":[{"name":"layers","mountPath":"/layers"}]}],"volumes":[{"name":"layers","persistentVolumeClaim":{"claimName":"astonish-layers"}}]}}`

	cmd := exec.Command("kubectl", "run", podName,
		"--image=alpine:3.21",
		"--restart=Never",
		"-n", ns,
		"--rm", "-it",
		"--overrides", overrides,
		"--", "sh", "-c", "sleep 1")

	out, err := cmd.CombinedOutput()
	if err != nil {
		// Non-fatal — the cluster may be under pressure; log and continue.
		t.Logf("[e2eboot] SweepStagingDirs: %v (output: %s)", err, strings.TrimSpace(string(out)))
	} else {
		t.Logf("[e2eboot] SweepStagingDirs: cleaned __staging-* from layers PVC in %s", ns)
	}
}

// SweepOrphanLayers aggressively removes layer directories on the astonish-layers PVC
// except for the well-known @base layer and the platform "Configure Base" layer.
//
// IMPORTANT: This function is **not** called automatically by SweepAllPods.
// Both @base (raw OS image) and the platform base layer (created by "Configure Base"
// during make e2e-k8s-up or on first use) are infrastructure and are intended to
// survive across multiple test runs within one e2e-k8s-up provisioning.
//
// Use this function only for manual recovery when you want to wipe all custom layers.
func SweepOrphanLayers(t *testing.T) {
	t.Helper()
	ns := SandboxNamespace()
	podName := fmt.Sprintf("sweep-layers-%d", time.Now().UnixNano()%100000000)

	// Remove everything except:
	//   - the sacred raw @base layer (UUID form and the @base directory)
	//   - any layer that is still referenced (we leave that to the GC reconciler in prod)
	overrides := `{"spec":{"restartPolicy":"Never","containers":[{"name":"sweep","image":"alpine:3.21","command":["sh","-c","find /layers -mindepth 1 -maxdepth 1 ! -name 'a0000000-0000-4000-8000-000000000001' ! -name '@base' -exec rm -rf {} + 2>/dev/null; echo 'orphan layers cleaned'"],"volumeMounts":[{"name":"layers","mountPath":"/layers"}]}],"volumes":[{"name":"layers","persistentVolumeClaim":{"claimName":"astonish-layers"}}]}}`

	cmd := exec.Command("kubectl", "run", podName,
		"--image=alpine:3.21",
		"--restart=Never",
		"-n", ns,
		"--rm", "-it",
		"--overrides", overrides,
		"--", "sh", "-c", "sleep 2")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("[e2eboot] SweepOrphanLayers: %v (output: %s)", err, strings.TrimSpace(string(out)))
	} else {
		t.Logf("[e2eboot] SweepOrphanLayers: cleaned unreferenced layers from PVC in %s", ns)
	}
}
