//go:build e2e

// Package sandbox_layerchain tests the full sandbox layer-chain pipeline
// end-to-end. It exercises 4 scenarios: fresh install, configured base,
// base + team template, and team template only.
//
// This test is NOT parallelizable — it mutates shared K8s PVCs and cluster
// state (Configure Base, team template layers).
//
// Run:
//
//	go test -tags=e2e -count=1 -v -timeout=10m ./tests/e2e/sandbox_layerchain/
package sandbox_layerchain

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/tests/e2eboot"
)

// COVERS: SANDBOX-001
func TestE2E_SandboxLayerChain(t *testing.T) {
	// NOT parallel — mutates shared cluster state.
	// In shared/inspector mode (ASTONISH_E2E_KEEP_ALIVE=1) this test is
	// incompatible with the rest of the suite because it mutates global
	// sandbox_templates rows ("base", "team-general") in the platform DB
	// and the global astonish-layers PVC. Skip cleanly so the developer
	// can still browse all the chat data; run this test in isolated mode.
	if os.Getenv("ASTONISH_E2E_KEEP_ALIVE") == "1" {
		t.Skip("SandboxLayerChain mutates global sandbox state; run with `make test-e2e` (isolated mode) instead.")
	}
	requireSandboxInfra(t)
	h := e2eboot.Bootstrap(t)

	// -------------------------------------------------------------------
	// Scenario 1: Fresh Install (no Configure Base, no team template)
	// -------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 1: Fresh Install ===")

	sessionID, podName := h.ChatAndWaitForPod(t, "Run this shell command and show me the output: echo SMOKE_OK")
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain := e2eboot.GetPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	e2eboot.AssertCommandAbsent(t, podName, "node", "node should NOT be present without Configure Base")
	e2eboot.AssertCommandAbsent(t, podName, "vi", "vi should NOT be present without team template")

	h.CleanupSession(t, sessionID, podName)

	// -------------------------------------------------------------------
	// Configure Base Sandbox
	// -------------------------------------------------------------------

	t.Log("")
	t.Log("=== Configure Base Sandbox ===")

	configureBaseSandbox(t, h)

	// -------------------------------------------------------------------
	// Scenario 2: Configured Base Only
	// -------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 2: Configured Base Only ===")

	sessionID, podName = h.ChatAndWaitForPod(t, "Run this shell command and show me the output: echo SMOKE_OK")
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain = e2eboot.GetPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	e2eboot.AssertCommandPresent(t, podName, "node", "node should be present from Configure Base")
	e2eboot.AssertCommandAbsent(t, podName, "vi", "vi should NOT be present without team template")

	h.CleanupSession(t, sessionID, podName)

	// -------------------------------------------------------------------
	// Create Team Template (install vim)
	// -------------------------------------------------------------------

	t.Log("")
	t.Log("=== Create Team Template ===")

	createAndSaveTeamTemplate(t, h)

	// -------------------------------------------------------------------
	// Scenario 3: Configured Base + Team Template
	// -------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 3: Configured Base + Team Template ===")

	sessionID, podName = h.ChatAndWaitForPod(t, "Run this shell command and show me the output: echo SMOKE_OK")
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain = e2eboot.GetPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	e2eboot.AssertCommandPresent(t, podName, "node", "node should be present from Configure Base")
	e2eboot.AssertCommandPresent(t, podName, "vi", "vi should be present from team template")

	h.CleanupSession(t, sessionID, podName)

	// -------------------------------------------------------------------
	// Reset @base to sentinel
	// -------------------------------------------------------------------

	t.Log("")
	t.Log("=== Reset @base ===")

	resetBaseSentinel(t, h)

	// -------------------------------------------------------------------
	// Scenario 4: Team Template Only
	// -------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 4: Team Template Only ===")

	sessionID, podName = h.ChatAndWaitForPod(t, "Run this shell command and show me the output: echo SMOKE_OK")
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain = e2eboot.GetPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	e2eboot.AssertCommandAbsent(t, podName, "node", "node should NOT be present with @base reset")
	e2eboot.AssertCommandPresent(t, podName, "vi", "vi should be present from team template")

	h.CleanupSession(t, sessionID, podName)

	t.Log("")
	t.Log("=== All 4 scenarios passed! ===")
}

// ---------------------------------------------------------------------------
// Configure Base Sandbox
// ---------------------------------------------------------------------------

func configureBaseSandbox(t *testing.T, h *e2eboot.Harness) {
	t.Helper()

	body := map[string]any{
		"core":           true,
		"optional_tools": []string{},
		"browser":        map[string]string{"engine": "none"},
		"extra_steps":    []string{},
		"architecture":   "amd64",
	}

	resp := h.PostWithTimeout(t, "/api/platform/admin/sandbox/base/configure", body, 5*time.Minute)
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("configure base failed: %d %s", resp.StatusCode, string(respBody))
	}

	// Read SSE stream until done
	scanner := bufio.NewScanner(resp.Body)
	var lastEvent string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			lastEvent = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			if lastEvent == "progress" {
				var progress struct {
					Message string `json:"message"`
				}
				_ = json.Unmarshal([]byte(data), &progress)
				t.Logf("  [configure-base] %s", progress.Message)
			} else if lastEvent == "done" {
				var done struct {
					Status  string `json:"status"`
					LayerID string `json:"layer_id"`
				}
				_ = json.Unmarshal([]byte(data), &done)
				if done.Status == "success" {
					t.Logf("  Configure Base completed: layer_id=%s", done.LayerID)
					return
				}
				t.Fatalf("Configure Base failed: %s", data)
			} else if lastEvent == "error" {
				t.Fatalf("Configure Base error: %s", data)
			}
		}
	}
	t.Fatal("Configure Base SSE stream ended without 'done' event")
}

// ---------------------------------------------------------------------------
// Team Template
// ---------------------------------------------------------------------------

func createAndSaveTeamTemplate(t *testing.T, h *e2eboot.Harness) {
	t.Helper()

	// Step 1: Create team template editor session
	t.Log("  Creating team template editor...")
	resp := h.Post(t, "/api/team/template/create", nil)
	resp.Body.Close()
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		t.Fatalf("create team template: status %d", resp.StatusCode)
	}

	// Wait for editor pod to be Running
	editorPod := "astn-sess-team-template-general"
	t.Logf("  Waiting for editor pod %s...", editorPod)
	e2eboot.WaitForPodRunning(t, editorPod, 120*time.Second)
	time.Sleep(5 * time.Second) // Wait for overlay composition

	// Step 2: Install vim inside the editor pod
	t.Log("  Installing vim in editor pod...")
	ns := e2eboot.SandboxNamespace()
	installCmd := exec.Command("kubectl", "exec", "-n", ns, editorPod, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c",
		"apt-get update -qq && apt-get install -y -qq vim </dev/null")
	output, err := installCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install vim in editor: %v\nOutput: %s", err, string(output))
	}
	t.Log("  vim installed successfully")

	// Verify vim is there
	e2eboot.AssertCommandPresent(t, editorPod, "vim", "vim installed in editor pod")

	// Step 3: Save team template
	t.Log("  Saving team template...")
	resp = h.Post(t, "/api/team/template/save", nil)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("save team template: %d %s", resp.StatusCode, string(respBody))
	}
	t.Log("  Team template saved successfully")
}

// ---------------------------------------------------------------------------
// Reset @base Sentinel
// ---------------------------------------------------------------------------

func resetBaseSentinel(t *testing.T, h *e2eboot.Harness) {
	t.Helper()
	ctx := context.Background()

	// Use the store interface (works for both PG and SQLite backends).
	tpl, err := h.PlatformBackend().SandboxTemplates().GetBySlug(ctx, store.SandboxTemplateScopeGlobal, "", "base")
	if err != nil {
		t.Fatalf("get @base template: %v", err)
	}
	if tpl == nil {
		t.Fatal("@base template not found")
	}

	baseVal := "@base"
	tpl.TopLayerID = &baseVal
	if err := h.PlatformBackend().SandboxTemplates().Update(ctx, tpl); err != nil {
		t.Fatalf("reset @base sentinel: %v", err)
	}

	// Verify the update took effect.
	tpl2, err := h.PlatformBackend().SandboxTemplates().GetBySlug(ctx, store.SandboxTemplateScopeGlobal, "", "base")
	if err != nil {
		t.Fatalf("verify reset: %v", err)
	}
	if tpl2 == nil || tpl2.TopLayerID == nil || *tpl2.TopLayerID != "@base" {
		topID := ""
		if tpl2 != nil && tpl2.TopLayerID != nil {
			topID = *tpl2.TopLayerID
		}
		t.Fatalf("reset failed: top_layer_id=%q, want '@base'", topID)
	}
	t.Log("  Reset @base.top_layer_id to sentinel '@base'")
}

// requireSandboxInfra checks that the K8s cluster has the sandbox namespace
// with the required PVCs. Skips the test if infrastructure is not available.
func requireSandboxInfra(t *testing.T) {
	t.Helper()
	ns := e2eboot.SandboxNamespace()

	// Check that kubectl is available and the namespace exists
	out, err := exec.Command("kubectl", "get", "namespace", ns, "-o", "jsonpath={.metadata.name}").Output()
	if err != nil || strings.TrimSpace(string(out)) != ns {
		t.Skipf("K8s namespace %q not found — skipping sandbox layer-chain test (requires cluster with sandbox infra)", ns)
	}

	// Check that required PVCs exist
	for _, pvc := range []string{"astonish-layers", "astonish-uppers"} {
		out, err = exec.Command("kubectl", "get", "pvc", "-n", ns, pvc, "-o", "jsonpath={.metadata.name}").Output()
		if err != nil || strings.TrimSpace(string(out)) != pvc {
			t.Skipf("PVC %q not found in namespace %q — skipping sandbox layer-chain test", pvc, ns)
		}
	}
}
