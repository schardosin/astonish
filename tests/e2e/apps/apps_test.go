//go:build e2e

// Package apps contains E2E tests for the Astonish Apps subsystem:
// CRUD persistence, app state SQL, and MCP data source invocation.
package apps

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/schardosin/astonish/tests/e2eboot"
)

// ---------------------------------------------------------------------------
// APPS-003: App CRUD — create, read, list, delete
// ---------------------------------------------------------------------------

// TestE2E_Apps_CRUD verifies the full app lifecycle through the REST API:
// create an app via PUT, read it back, verify it in the list, delete it,
// and confirm it's gone.
//
// COVERS: APPS-003
func TestE2E_Apps_CRUD(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	const appName = "e2e_crud_test_app"
	const appCode = `export default function App() { return <div>Hello E2E</div>; }`
	const appDesc = "E2E test app for CRUD verification"

	// --- Create ---
	createBody := map[string]any{
		"code":        appCode,
		"description": appDesc,
		"version":     1,
		"sessionId":   "e2e-test-session",
	}
	resp := h.Put(t, "/api/apps/"+appName, createBody)
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/apps/%s returned %d: %s", appName, resp.StatusCode, body)
	}

	var createResult map[string]any
	if err := json.Unmarshal([]byte(body), &createResult); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if createResult["status"] != "ok" {
		t.Fatalf("expected status=ok, got %v", createResult["status"])
	}

	// --- Read ---
	resp = h.Get(t, "/api/apps/"+appName)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/apps/%s returned %d: %s", appName, resp.StatusCode, body)
	}

	var app map[string]any
	if err := json.Unmarshal([]byte(body), &app); err != nil {
		t.Fatalf("decode app: %v", err)
	}
	if app["code"] != appCode {
		t.Errorf("code mismatch: got %q", app["code"])
	}
	if app["description"] != appDesc {
		t.Errorf("description mismatch: got %q", app["description"])
	}

	// --- List ---
	resp = h.Get(t, "/api/apps")
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/apps returned %d: %s", resp.StatusCode, body)
	}

	var listResult struct {
		Apps []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Version     int    `json:"version"`
		} `json:"apps"`
	}
	if err := json.Unmarshal([]byte(body), &listResult); err != nil {
		t.Fatalf("decode apps list: %v", err)
	}

	found := false
	for _, a := range listResult.Apps {
		if a.Name == appName {
			found = true
			if a.Description != appDesc {
				t.Errorf("list description mismatch: got %q", a.Description)
			}
			if a.Version != 1 {
				t.Errorf("list version mismatch: got %d", a.Version)
			}
			break
		}
	}
	if !found {
		t.Fatalf("app %q not found in list (got %d apps)", appName, len(listResult.Apps))
	}

	// --- Delete ---
	resp = h.Delete(t, "/api/apps/"+appName)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/apps/%s returned %d: %s", appName, resp.StatusCode, body)
	}

	// --- Confirm gone ---
	resp = h.Get(t, "/api/apps/"+appName)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("app should be deleted but GET returned 200: %s", body)
	}
}

// ---------------------------------------------------------------------------
// APPS-007a: App State Persistence — SQL exec/query lifecycle
// ---------------------------------------------------------------------------

// TestE2E_Apps_StatePersistence verifies that an app's per-app database
// supports arbitrary DDL/DML: CREATE TABLE, INSERT, SELECT, and that
// deleting the app also destroys its state database.
//
// COVERS: APPS-007
func TestE2E_Apps_StatePersistence(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	const appName = "e2e_state_test_app"

	// Create the app first.
	createBody := map[string]any{
		"code":        "export default function App() { return <div>State Test</div>; }",
		"description": "E2E state persistence test",
		"version":     1,
	}
	resp := h.Put(t, "/api/apps/"+appName, createBody)
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/apps/%s returned %d: %s", appName, resp.StatusCode, body)
	}

	// --- CREATE TABLE ---
	execResp := appStateExec(t, h, appName, "CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL, created_at DATETIME DEFAULT CURRENT_TIMESTAMP)", nil)
	if execResp.Error != "" {
		t.Fatalf("CREATE TABLE failed: %s", execResp.Error)
	}

	// --- INSERT ---
	execResp = appStateExec(t, h, appName, "INSERT INTO items (name) VALUES (?)", []any{"hello-e2e"})
	if execResp.Error != "" {
		t.Fatalf("INSERT failed: %s", execResp.Error)
	}
	// Verify rowsAffected
	if data, ok := execResp.Data.(map[string]any); ok {
		if ra, _ := data["rowsAffected"].(float64); ra != 1 {
			t.Errorf("expected rowsAffected=1, got %v", ra)
		}
	}

	// --- INSERT another row ---
	execResp = appStateExec(t, h, appName, "INSERT INTO items (name) VALUES (?)", []any{"world-e2e"})
	if execResp.Error != "" {
		t.Fatalf("INSERT #2 failed: %s", execResp.Error)
	}

	// --- SELECT ---
	queryResp := appStateQuery(t, h, appName, "SELECT id, name FROM items ORDER BY id", nil)
	if queryResp.Error != "" {
		t.Fatalf("SELECT failed: %s", queryResp.Error)
	}

	rows, ok := queryResp.Data.([]any)
	if !ok {
		t.Fatalf("expected data to be array, got %T", queryResp.Data)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}

	// Verify first row
	row0, _ := rows[0].(map[string]any)
	if row0["name"] != "hello-e2e" {
		t.Errorf("row[0].name = %v, want 'hello-e2e'", row0["name"])
	}

	// Verify second row
	row1, _ := rows[1].(map[string]any)
	if row1["name"] != "world-e2e" {
		t.Errorf("row[1].name = %v, want 'world-e2e'", row1["name"])
	}

	// --- DELETE the app ---
	resp = h.Delete(t, "/api/apps/"+appName)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("DELETE /api/apps/%s returned %d: %s", appName, resp.StatusCode, body)
	}

	// --- Verify state is gone ---
	// After deleting the app, the per-app state DB should be dropped.
	// A query against it should fail.
	queryResp = appStateQuery(t, h, appName, "SELECT * FROM items", nil)
	if queryResp.Error == "" {
		t.Fatalf("expected query to fail after app deletion, but it succeeded with data: %v", queryResp.Data)
	}
}

// ---------------------------------------------------------------------------
// APPS-007b: MCP Data Source — real Tavily tool invocation from app
// ---------------------------------------------------------------------------

// TestE2E_Apps_MCPDataSource installs the Tavily MCP server with a real API
// key, creates an app, and invokes tavily_search through the app data handler.
// This exercises the full stdio-in-sandbox MCP invocation path.
//
// Requirements:
//   - TAVILY_API_KEY env var set to a valid Tavily API key
//   - K8s sandbox infrastructure running (sandbox enabled in config)
//
// COVERS: APPS-007
func TestE2E_Apps_MCPDataSource(t *testing.T) {
	tavilyKey := os.Getenv("TAVILY_API_KEY")
	if tavilyKey == "" {
		t.Skip("TAVILY_API_KEY not set — skipping MCP data source test")
	}

	h := e2eboot.Bootstrap(t)

	// Ensure the base sandbox has Node.js installed (required for npx-based
	// MCP servers like Tavily). This is idempotent — if the base is already
	// configured (top_layer_id != "@base"), it skips the build.
	ensureBaseConfigured(t, h)

	const appName = "e2e_mcp_test_app"

	// --- Install Tavily at platform scope with real API key ---
	installBody := map[string]any{
		"env": map[string]string{
			"TAVILY_API_KEY": tavilyKey,
		},
	}
	resp := h.Post(t, "/api/standard-servers/tavily/install?scope=platform", installBody)
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Tavily install returned %d: %s", resp.StatusCode, body)
	}

	// Wait for tavily_search to appear in the tools list. Discovery runs
	// asynchronously in a background goroutine after install returns.
	e2eboot.PollUntil(t, "tavily_search visible in /api/tools", 3*time.Minute, 2*time.Second, func() bool {
		return hasToolInToolsList(t, h, "tavily_search")
	})

	// --- Create the app ---
	createBody := map[string]any{
		"code":        "export default function App() { return <div>MCP Test</div>; }",
		"description": "E2E MCP data source test",
		"version":     1,
	}
	resp = h.Put(t, "/api/apps/"+appName, createBody)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PUT /api/apps/%s returned %d: %s", appName, resp.StatusCode, body)
	}

	// --- Invoke tavily_search via app data handler ---
	dataBody := map[string]any{
		"sourceId":  "mcp:tavily/tavily_search",
		"args":      map[string]any{"query": "what is the Go programming language"},
		"requestId": "e2e-mcp-test-1",
	}
	resp = h.PostWithTimeout(t, "/api/apps/data", dataBody, 2*time.Minute)
	body = e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/apps/data returned %d: %s", resp.StatusCode, body)
	}

	var dataResp struct {
		RequestID string `json:"requestId"`
		Data      any    `json:"data"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &dataResp); err != nil {
		t.Fatalf("decode data response: %v", err)
	}

	if dataResp.Error != "" {
		t.Fatalf("MCP data source returned error: %s", dataResp.Error)
	}

	if dataResp.Data == nil {
		t.Fatal("MCP data source returned nil data")
	}

	// The response should contain search results. Tavily returns structured
	// data with results containing URLs and content. Just verify it's non-empty.
	dataJSON, _ := json.Marshal(dataResp.Data)
	if len(dataJSON) < 50 {
		t.Fatalf("MCP data source response too short (expected real search results): %s", string(dataJSON))
	}

	t.Logf("Tavily search returned %d bytes of data", len(dataJSON))

	// --- Cleanup ---
	resp = h.Delete(t, "/api/apps/"+appName)
	e2eboot.ReadBody(t, resp)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

type stateResponse struct {
	RequestID string `json:"requestId"`
	Data      any    `json:"data"`
	Error     string `json:"error"`
}

func appStateExec(t *testing.T, h *e2eboot.Harness, appName, sql string, params []any) stateResponse {
	t.Helper()
	body := map[string]any{
		"appName":   appName,
		"sql":       sql,
		"params":    params,
		"requestId": "e2e-exec",
	}
	resp := h.Post(t, "/api/apps/state/exec", body)
	raw := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/apps/state/exec returned %d: %s", resp.StatusCode, raw)
	}
	var result stateResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode exec response: %v", err)
	}
	return result
}

func appStateQuery(t *testing.T, h *e2eboot.Harness, appName, sql string, params []any) stateResponse {
	t.Helper()
	body := map[string]any{
		"appName":   appName,
		"sql":       sql,
		"params":    params,
		"requestId": "e2e-query",
	}
	resp := h.Post(t, "/api/apps/state/query", body)
	raw := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("POST /api/apps/state/query returned %d: %s", resp.StatusCode, raw)
	}
	var result stateResponse
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		t.Fatalf("decode query response: %v", err)
	}
	return result
}

func hasToolInToolsList(t *testing.T, h *e2eboot.Harness, toolName string) bool {
	t.Helper()
	resp := h.Get(t, "/api/tools")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var payload struct {
		Tools []struct {
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false
	}
	for _, tl := range payload.Tools {
		if tl.Name == toolName {
			return true
		}
	}
	return false
}

// ensureBaseConfigured checks whether the base sandbox template has been
// configured with runtimes (Node.js, etc.). If the base only has the bare
// "@base" layer, it triggers a "Configure Base" build with core=true which
// installs Node.js. This is idempotent — skips if already configured.
func ensureBaseConfigured(t *testing.T, h *e2eboot.Harness) {
	t.Helper()

	// OpenShell sandboxes have all tools (Node.js, npx, etc.) baked into the
	// container image at build time. There is no "Configure Base" PVC overlay
	// concept — skip entirely.
	if e2eboot.SandboxBackendName() == "openshell" {
		t.Log("OpenShell backend — base image has tools pre-baked, skipping Configure Base")
		return
	}

	// Check current base template status via the admin API.
	resp := h.Get(t, "/api/platform/admin/sandbox/base")
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/platform/admin/sandbox/base returned %d: %s", resp.StatusCode, body)
	}

	var baseInfo struct {
		LayerID string `json:"layer_id"`
	}
	if err := json.Unmarshal([]byte(body), &baseInfo); err != nil {
		t.Fatalf("decode base info: %v", err)
	}

	// If layer_id is a real content-addressed hash (not empty or "@base"),
	// the base has already been configured with a layer that includes Node.js.
	if baseInfo.LayerID != "" && baseInfo.LayerID != "@base" {
		t.Logf("Base sandbox already configured (layer=%s), skipping build", baseInfo.LayerID[:12])
		return
	}

	t.Log("Base sandbox not configured — running Configure Base (core=true) to install Node.js...")

	configBody := map[string]any{
		"core":           true,
		"optional_tools": []string{},
		"browser":        map[string]string{"engine": "none"},
		"extra_steps":    []string{},
		"architecture":   "amd64",
	}

	resp = h.PostWithTimeout(t, "/api/platform/admin/sandbox/base/configure", configBody, 5*time.Minute)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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
