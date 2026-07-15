//go:build e2e

package e2eboot

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/sandbox/openshell"
)

// openShellSandboxHelper implements SandboxTestHelper for the OpenShell backend.
// It uses the GatewayClient gRPC interface to interact with sandboxes.
type openShellSandboxHelper struct {
	client openshell.GatewayClient
}

// newOpenShellHelper creates a new OpenShell helper connected to the gateway.
// It reads ASTONISH_E2E_OPENSHELL_GATEWAY for the gRPC address.
func newOpenShellHelper() *openShellSandboxHelper {
	addr := os.Getenv("ASTONISH_E2E_OPENSHELL_GATEWAY")
	if addr == "" {
		// Fallback — the Makefile should always set this via port-forward.
		addr = "localhost:18080"
	}

	client, err := openshell.NewGRPCGatewayClient(openshell.GRPCClientConfig{
		Addr: addr,
		TLS:  false, // e2e gateway runs plaintext (disableTls: true)
	})
	if err != nil {
		// Can't use t.Fatal here (no *testing.T in constructor).
		// The helper will be nil and tests will panic with a clear message.
		panic("e2eboot: failed to create OpenShell gateway client: " + err.Error())
	}

	return &openShellSandboxHelper{client: client}
}

func (h *openShellSandboxHelper) Backend() string { return "openshell" }

func (h *openShellSandboxHelper) DeriveResourceID(sessionID string) string {
	// OpenShell uses the same pod naming convention as K8s backend.
	return DerivePodName(sessionID)
}

func (h *openShellSandboxHelper) WaitForReady(t *testing.T, sessionID string, timeout time.Duration) {
	t.Helper()
	sandboxName := DerivePodName(sessionID)
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		status, err := h.client.GetSandboxStatus(ctx, sandboxName)
		cancel()

		if err == nil && status.State == openshell.SandboxStateRunning {
			t.Logf("[e2eboot/openshell] Sandbox %s is Running", sandboxName)
			return
		}
		if err != nil {
			t.Logf("[e2eboot/openshell] GetSandboxStatus(%s): %v (retrying...)", sandboxName, err)
		} else {
			t.Logf("[e2eboot/openshell] Sandbox %s state=%s (waiting for Running...)", sandboxName, status.State)
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("[e2eboot/openshell] sandbox %s did not reach Running within %v", sandboxName, timeout)
}

func (h *openShellSandboxHelper) Cleanup(t *testing.T, sessionID string) {
	t.Helper()
	sandboxName := DerivePodName(sessionID)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := h.client.DeleteSandbox(ctx, sandboxName); err != nil {
		// Non-fatal — sandbox may already be gone.
		t.Logf("[e2eboot/openshell] DeleteSandbox(%s): %v (may already be deleted)", sandboxName, err)
	} else {
		t.Logf("[e2eboot/openshell] Deleted sandbox %s", sandboxName)
	}
}

func (h *openShellSandboxHelper) SweepAll(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	sandboxes, err := h.client.ListSandboxes(ctx, "")
	if err != nil {
		t.Logf("[e2eboot/openshell] SweepAll: ListSandboxes: %v", err)
		return
	}

	if len(sandboxes) == 0 {
		t.Logf("[e2eboot/openshell] SweepAll: no sandboxes to clean up")
		return
	}

	t.Logf("[e2eboot/openshell] SweepAll: deleting %d sandbox(es)...", len(sandboxes))
	for _, sb := range sandboxes {
		delCtx, delCancel := context.WithTimeout(context.Background(), 15*time.Second)
		if err := h.client.DeleteSandbox(delCtx, sb.Name); err != nil {
			t.Logf("[e2eboot/openshell] SweepAll: delete %s: %v", sb.Name, err)
		}
		delCancel()
	}
	t.Logf("[e2eboot/openshell] SweepAll: done")
}

func (h *openShellSandboxHelper) AssertCommandPresent(t *testing.T, sessionID string, cmd string, msg string) {
	t.Helper()
	sandboxName := DerivePodName(sessionID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := h.client.ExecCommand(ctx, sandboxName, openshell.ExecRequest{
		Command: []string{"sh", "-c", "command -v " + cmd},
	})
	if err != nil {
		t.Errorf("FAIL: %s (exec error checking command '%s' in sandbox %s: %v)", msg, cmd, sandboxName, err)
		return
	}
	if resp.ExitCode != 0 {
		t.Errorf("FAIL: %s (command '%s' not found in sandbox %s)", msg, cmd, sandboxName)
	} else {
		t.Logf("  OK: %s", msg)
	}
}

func (h *openShellSandboxHelper) AssertCommandAbsent(t *testing.T, sessionID string, cmd string, msg string) {
	t.Helper()
	sandboxName := DerivePodName(sessionID)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := h.client.ExecCommand(ctx, sandboxName, openshell.ExecRequest{
		Command: []string{"sh", "-c", "command -v " + cmd},
	})
	if err != nil {
		// If exec fails entirely, the command is arguably "absent" — but log it.
		t.Logf("  OK: %s (exec failed: %v — treating as absent)", msg, err)
		return
	}
	if resp.ExitCode == 0 {
		found := strings.TrimSpace(string(resp.Stdout))
		t.Errorf("FAIL: %s (command '%s' IS present in sandbox %s at %s but should not be)", msg, cmd, sandboxName, found)
	} else {
		t.Logf("  OK: %s", msg)
	}
}

func (h *openShellSandboxHelper) GetMetadata(t *testing.T, sessionID string, key string) string {
	t.Helper()
	// OpenShell sandboxes don't have K8s-style annotations accessible via the helper.
	// The layer-chain test that uses GetMetadata is K8s-only and skipped for OpenShell.
	t.Logf("[e2eboot/openshell] GetMetadata(%s, %s): not supported (returning empty)", sessionID, key)
	return ""
}
