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
}
