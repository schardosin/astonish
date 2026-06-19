//go:build e2e

package e2eboot

import (
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// k8sSandboxHelper implements SandboxTestHelper for the K8s backend.
// It wraps the existing kubectl-based helper functions from sandbox.go.
type k8sSandboxHelper struct{}

func (h *k8sSandboxHelper) Backend() string { return "k8s" }

func (h *k8sSandboxHelper) DeriveResourceID(sessionID string) string {
	return DerivePodName(sessionID)
}

func (h *k8sSandboxHelper) WaitForReady(t *testing.T, sessionID string, timeout time.Duration) {
	t.Helper()
	podName := DerivePodName(sessionID)
	WaitForPodRunning(t, podName, timeout)
	// Give overlay composition a moment to settle.
	time.Sleep(3 * time.Second)
}

func (h *k8sSandboxHelper) Cleanup(t *testing.T, sessionID string) {
	t.Helper()
	podName := DerivePodName(sessionID)
	DeletePod(t, podName)
}

func (h *k8sSandboxHelper) SweepAll(t *testing.T) {
	t.Helper()
	SweepAllPods(t)
}

func (h *k8sSandboxHelper) AssertCommandPresent(t *testing.T, sessionID string, cmd string, msg string) {
	t.Helper()
	podName := DerivePodName(sessionID)
	ns := SandboxNamespace()
	err := exec.Command("kubectl", "exec", "-n", ns, podName, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c", "command -v "+cmd).Run()
	if err != nil {
		t.Errorf("FAIL: %s (command '%s' not found in pod %s)", msg, cmd, podName)
	} else {
		t.Logf("  OK: %s", msg)
	}
}

func (h *k8sSandboxHelper) AssertCommandAbsent(t *testing.T, sessionID string, cmd string, msg string) {
	t.Helper()
	podName := DerivePodName(sessionID)
	ns := SandboxNamespace()
	err := exec.Command("kubectl", "exec", "-n", ns, podName, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c", "command -v "+cmd).Run()
	if err == nil {
		t.Errorf("FAIL: %s (command '%s' IS present in pod %s but should not be)", msg, cmd, podName)
	} else {
		t.Logf("  OK: %s", msg)
	}
}

func (h *k8sSandboxHelper) GetMetadata(t *testing.T, sessionID string, key string) string {
	t.Helper()
	podName := DerivePodName(sessionID)
	ns := SandboxNamespace()
	jsonPath := fmt.Sprintf(`{.metadata.annotations.%s}`, key)
	out, err := exec.Command("kubectl", "get", "pod", "-n", ns, podName,
		"-o", fmt.Sprintf("jsonpath=%s", jsonPath)).Output()
	if err != nil {
		t.Fatalf("[e2eboot] get pod annotation %s on %s: %v", key, podName, err)
	}
	return strings.TrimSpace(string(out))
}
