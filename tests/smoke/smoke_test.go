//go:build integration

// Package smoke contains the self-contained end-to-end smoke test for the
// sandbox layer-chain pipeline. It bootstraps a fresh Astonish platform
// instance (suffix "smoke"), starts a local server, exercises all 4
// layer-chain scenarios, and tears down cleanly.
//
// Prerequisites:
//   - ASTONISH_TEST_DSN: Postgres admin connection string
//   - kubectl access to a K8s cluster with the sandbox namespace
//   - BIFROST_API_KEY (or equivalent provider key) in the environment
//   - The sandbox image and PVCs must already exist in the cluster
//
// Run:
//
//	go test -tags=integration -count=1 -v -timeout=10m ./tests/smoke/ -run TestSmokeLayerChain
package smoke

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/schardosin/astonish/pkg/api"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/launcher"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

const (
	smokeSuffix    = "smoke"
	smokeNamespace = "astonish-sandbox"
	smokeEmail     = "smoke@test.local"
	smokePassword  = "SmokeTest2024!"
	smokeJWTSecret = "smoke-test-jwt-secret-that-is-at-least-32-chars-long"
)

func TestSmokeLayerChain(t *testing.T) {
	dsn := os.Getenv("ASTONISH_TEST_DSN")
	if dsn == "" {
		t.Skip("ASTONISH_TEST_DSN not set — skipping E2E smoke test")
	}

	ctx := context.Background()

	// -----------------------------------------------------------------------
	// Phase 0: Setup — fresh platform database + server
	// -----------------------------------------------------------------------

	t.Log("=== Phase 0: Setup ===")

	// Drop any leftover smoke databases from a previous failed run
	dropSmokeDBs(t, ctx, dsn)

	// Bootstrap fresh platform
	t.Log("Bootstrapping fresh platform database (suffix=smoke)...")
	if err := pgstore.BootstrapPlatform(ctx, dsn, smokeSuffix); err != nil {
		t.Fatalf("BootstrapPlatform: %v", err)
	}
	t.Cleanup(func() { dropSmokeDBs(t, ctx, dsn) })

	// Write a temp config.yaml
	configDir := t.TempDir()
	astonishDir := filepath.Join(configDir, "astonish")
	if err := os.MkdirAll(astonishDir, 0755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	platformDSN := buildSmokeDSN(t, dsn)
	writeTestConfig(t, astonishDir, platformDSN)

	// Override config path via XDG_CONFIG_HOME
	t.Setenv("XDG_CONFIG_HOME", configDir)

	// Connect PGStore
	pgCfg := config.PostgresConfig{
		PlatformDSN:    platformDSN,
		InstanceSuffix: smokeSuffix,
	}
	svc, pgStore, err := pgstore.NewPlatformServices(ctx, pgCfg)
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	t.Cleanup(func() { pgStore.Close() })

	// Seed the Bifrost provider in platform settings (in platform mode,
	// providers come from DB not config.yaml).
	seedPlatformProvider(t, ctx, pgStore)

	// Create PlatformAuth
	authCfg := config.PlatformAuthConfig{
		JWTSecret: smokeJWTSecret,
	}
	storageCfg := config.StorageConfig{
		Backend:  "postgres",
		Postgres: pgCfg,
		Auth:     authCfg,
	}
	platformAuth := api.NewPlatformAuth(authCfg, pgStore, storageCfg)

	// Start server
	studio, err := launcher.NewStudioServer(0,
		launcher.WithServices(svc),
		launcher.WithPlatformAuth(platformAuth, pgStore),
	)
	if err != nil {
		t.Fatalf("NewStudioServer: %v", err)
	}

	go func() {
		if serveErr := studio.Serve(); serveErr != nil && serveErr != http.ErrServerClosed {
			t.Logf("Studio server error: %v", serveErr)
		}
	}()
	t.Cleanup(func() {
		shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		_ = studio.Shutdown(shutdownCtx)
	})

	// Give the server a moment to start
	time.Sleep(500 * time.Millisecond)

	baseURL := fmt.Sprintf("http://localhost:%d", studio.Port())
	t.Logf("Server running at %s", baseURL)

	// Register first user (auto-creates org "default" + team "general")
	t.Log("Registering first user...")
	registerUser(t, baseURL)

	// Login to get access token
	token := loginUser(t, baseURL)
	t.Logf("Got access token: %s...", token[:20])

	// -----------------------------------------------------------------------
	// Phase 1: Scenario 1 — Fresh Install (no Configure Base, no team template)
	// -----------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 1: Fresh Install ===")

	sessionID, podName := createChatAndWaitForPod(t, baseURL, token)
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain := getPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	assertCommandAbsent(t, podName, "node", "node should NOT be present without Configure Base")
	assertCommandAbsent(t, podName, "vi", "vi should NOT be present without team template")

	cleanupSession(t, baseURL, token, sessionID, podName)

	// -----------------------------------------------------------------------
	// Phase 2: Configure Base Sandbox
	// -----------------------------------------------------------------------

	t.Log("")
	t.Log("=== Phase 2: Configure Base Sandbox ===")

	configureBaseSandbox(t, baseURL, token)

	// -----------------------------------------------------------------------
	// Phase 3: Scenario 2 — Configured Base Only
	// -----------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 2: Configured Base Only ===")

	sessionID, podName = createChatAndWaitForPod(t, baseURL, token)
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain = getPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	assertCommandPresent(t, podName, "node", "node should be present from Configure Base")
	assertCommandAbsent(t, podName, "vi", "vi should NOT be present without team template")

	cleanupSession(t, baseURL, token, sessionID, podName)

	// -----------------------------------------------------------------------
	// Phase 4: Create Team Template (install vim)
	// -----------------------------------------------------------------------

	t.Log("")
	t.Log("=== Phase 4: Create Team Template ===")

	createAndSaveTeamTemplate(t, baseURL, token)

	// -----------------------------------------------------------------------
	// Phase 5: Scenario 3 — Configured Base + Team Template
	// -----------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 3: Configured Base + Team Template ===")

	sessionID, podName = createChatAndWaitForPod(t, baseURL, token)
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain = getPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	assertCommandPresent(t, podName, "node", "node should be present from Configure Base")
	assertCommandPresent(t, podName, "vi", "vi should be present from team template")

	cleanupSession(t, baseURL, token, sessionID, podName)

	// -----------------------------------------------------------------------
	// Phase 6: Reset @base to sentinel
	// -----------------------------------------------------------------------

	t.Log("")
	t.Log("=== Phase 6: Reset @base ===")

	resetBaseSentinel(t, ctx, platformDSN)

	// -----------------------------------------------------------------------
	// Phase 7: Scenario 4 — Team Template Only
	// -----------------------------------------------------------------------

	t.Log("")
	t.Log("=== Scenario 4: Team Template Only ===")

	sessionID, podName = createChatAndWaitForPod(t, baseURL, token)
	t.Logf("Session: %s, Pod: %s", sessionID, podName)

	chain = getPodChain(t, podName)
	t.Logf("Chain: %s", chain)

	assertCommandAbsent(t, podName, "node", "node should NOT be present with @base reset")
	assertCommandPresent(t, podName, "vi", "vi should be present from team template")

	cleanupSession(t, baseURL, token, sessionID, podName)

	t.Log("")
	t.Log("=== All 4 scenarios passed! ===")
}

// ---------------------------------------------------------------------------
// Setup Helpers
// ---------------------------------------------------------------------------

func buildSmokeDSN(t *testing.T, baseDSN string) string {
	t.Helper()
	dbName := config.PlatformDBName(smokeSuffix)
	result, err := pgstore.ReplaceDSNDatabase(baseDSN, dbName)
	if err != nil {
		t.Fatalf("ReplaceDSNDatabase: %v", err)
	}
	return result
}

func dropSmokeDBs(t *testing.T, ctx context.Context, baseDSN string) {
	t.Helper()
	adminDSN, err := pgstore.ReplaceDSNDatabase(baseDSN, "postgres")
	if err != nil {
		t.Logf("WARN: ReplaceDSNDatabase for admin: %v", err)
		return
	}
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Logf("WARN: connect to postgres admin: %v", err)
		return
	}
	defer conn.Close(ctx)

	dbs := []string{
		config.PlatformDBName(smokeSuffix),
		config.OrgDBName(smokeSuffix, "default"),
	}
	for _, db := range dbs {
		_, _ = conn.Exec(ctx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s'", db))
		_, err = conn.Exec(ctx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", db))
		if err != nil {
			t.Logf("WARN: drop %s: %v", db, err)
		} else {
			t.Logf("Dropped database %s", db)
		}
	}
}

func writeTestConfig(t *testing.T, dir, platformDSN string) {
	t.Helper()

	// Use the Bifrost provider from the dev environment
	bifrostURL := os.Getenv("BIFROST_BASE_URL")
	if bifrostURL == "" {
		bifrostURL = "https://bifrost.local.muxpie.com"
	}

	configYAML := fmt.Sprintf(`general:
  default_provider: Bifrost
  default_model: sapaicore/anthropic--claude-4.6-opus
providers:
  Bifrost:
    base_url: %s
    type: openai_compat
storage:
  backend: postgres
  postgres:
    platform_dsn: "%s"
    instance_suffix: %s
  auth:
    mode: builtin
    jwt_secret: %s
sandbox:
  enabled: true
  backend: k8s
  limits:
    memory: 2GB
    cpu: 2
    processes: 500
  kubernetes:
    kubeconfig_path: /root/.kube/config
    namespace: %s
    control_plane_namespace: astonish
    overlay_mode: fuse
    privileged_pods: true
    sandbox_image: schardosin/astonish-sandbox-base:dev
    layers_pvc_name: astonish-layers
    uppers_pvc_name: astonish-uppers
`, bifrostURL, platformDSN, smokeSuffix, smokeJWTSecret, smokeNamespace)

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(configYAML), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	t.Logf("Wrote test config to %s", path)
}

func registerUser(t *testing.T, baseURL string) {
	t.Helper()
	body := map[string]string{
		"email":    smokeEmail,
		"password": smokePassword,
	}
	resp := doPost(t, baseURL+"/api/auth/register", body, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("register failed: %d %s", resp.StatusCode, string(respBody))
	}
	t.Log("User registered successfully")
}

func loginUser(t *testing.T, baseURL string) string {
	t.Helper()
	body := map[string]string{
		"email":       smokeEmail,
		"password":    smokePassword,
		"client_type": "cli",
	}
	resp := doPost(t, baseURL+"/api/auth/login", body, "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("login failed: %d %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode login response: %v", err)
	}
	if result.AccessToken == "" {
		t.Fatal("login returned empty access_token")
	}
	return result.AccessToken
}

// ---------------------------------------------------------------------------
// Chat Session Helpers
// ---------------------------------------------------------------------------

func createChatAndWaitForPod(t *testing.T, baseURL, token string) (sessionID, podName string) {
	t.Helper()

	// Send a chat message that triggers a tool call
	body := map[string]string{
		"message": "Run this shell command and show me the output: echo SMOKE_OK",
	}
	jsonBody, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", baseURL+"/api/studio/chat", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("create chat request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Astonish-Team", "general")

	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("chat request failed: %d %s", resp.StatusCode, string(respBody))
	}

	// Parse SSE stream to get session ID
	sessionID = extractSessionIDFromSSE(t, resp.Body)
	if sessionID == "" {
		t.Fatal("failed to extract sessionId from SSE stream")
	}

	// Derive pod name
	podName = derivePodName(sessionID)
	t.Logf("Waiting for pod %s...", podName)

	// Wait for pod to be Running
	waitForPodRunning(t, podName, 120*time.Second)

	// Give overlay composition a moment
	time.Sleep(3 * time.Second)

	return sessionID, podName
}

func extractSessionIDFromSSE(t *testing.T, body io.Reader) string {
	t.Helper()
	scanner := bufio.NewScanner(body)
	var lastEventType string
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			lastEventType = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") && lastEventType == "session" {
			data := strings.TrimPrefix(line, "data: ")
			var session struct {
				SessionID string `json:"sessionId"`
			}
			if err := json.Unmarshal([]byte(data), &session); err == nil && session.SessionID != "" {
				return session.SessionID
			}
		}
		// Stop after done event (stream complete)
		if lastEventType == "done" {
			break
		}
	}
	return ""
}

func derivePodName(sessionID string) string {
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
	// Trim leading/trailing dashes
	s := strings.Trim(string(out), "-")
	if len(s) > maxIDLen {
		s = s[:maxIDLen]
	}
	s = strings.TrimRight(s, "-")
	return prefix + s
}

func waitForPodRunning(t *testing.T, podName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("kubectl", "get", "pod", "-n", smokeNamespace, podName,
			"-o", "jsonpath={.status.phase}").Output()
		if err == nil && strings.TrimSpace(string(out)) == "Running" {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("pod %s did not reach Running within %v", podName, timeout)
}

func getPodChain(t *testing.T, podName string) string {
	t.Helper()
	out, err := exec.Command("kubectl", "get", "pod", "-n", smokeNamespace, podName,
		"-o", `jsonpath={.metadata.annotations.astonish\.io/layer-chain}`).Output()
	if err != nil {
		t.Fatalf("get pod chain: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func cleanupSession(t *testing.T, baseURL, token, sessionID, podName string) {
	t.Helper()

	// Delete session via API
	req, _ := http.NewRequest("DELETE", baseURL+"/api/studio/sessions/"+sessionID, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Astonish-Team", "general")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err == nil {
		resp.Body.Close()
	}

	// Also delete the pod directly for immediate cleanup
	_ = exec.Command("kubectl", "delete", "pod", "-n", smokeNamespace, podName,
		"--grace-period=5", "--ignore-not-found").Run()

	// Wait for pod to disappear
	for i := 0; i < 10; i++ {
		out, err := exec.Command("kubectl", "get", "pod", "-n", smokeNamespace, podName,
			"-o", "jsonpath={.status.phase}").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			break
		}
		time.Sleep(2 * time.Second)
	}
}

// ---------------------------------------------------------------------------
// Tool Verification
// ---------------------------------------------------------------------------

func assertCommandPresent(t *testing.T, podName, cmd, msg string) {
	t.Helper()
	err := exec.Command("kubectl", "exec", "-n", smokeNamespace, podName, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c", "command -v "+cmd).Run()
	if err != nil {
		t.Errorf("FAIL: %s (command '%s' not found in pod %s)", msg, cmd, podName)
	} else {
		t.Logf("  ✓ %s", msg)
	}
}

func assertCommandAbsent(t *testing.T, podName, cmd, msg string) {
	t.Helper()
	err := exec.Command("kubectl", "exec", "-n", smokeNamespace, podName, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c", "command -v "+cmd).Run()
	if err == nil {
		t.Errorf("FAIL: %s (command '%s' IS present in pod %s but should not be)", msg, cmd, podName)
	} else {
		t.Logf("  ✓ %s", msg)
	}
}

// ---------------------------------------------------------------------------
// Configure Base Sandbox
// ---------------------------------------------------------------------------

func configureBaseSandbox(t *testing.T, baseURL, token string) {
	t.Helper()

	body := map[string]any{
		"core":           true,
		"optional_tools": []string{},
		"browser":        map[string]string{"engine": "none"},
		"extra_steps":    []string{},
		"architecture":   "amd64",
	}

	jsonBody, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", baseURL+"/api/platform/admin/sandbox/base/configure", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("configure base request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Astonish-Team", "general")

	// This is an SSE endpoint; stream until "done" event
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("configure base request: %v", err)
	}
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

// ---------------------------------------------------------------------------
// Team Template
// ---------------------------------------------------------------------------

func createAndSaveTeamTemplate(t *testing.T, baseURL, token string) {
	t.Helper()

	// Step 1: Create team template editor session
	t.Log("  Creating team template editor...")
	resp := doPost(t, baseURL+"/api/team/template/create", nil, token)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		t.Fatalf("create team template: status %d", resp.StatusCode)
	}

	// Wait for editor pod to be Running
	editorPod := "astn-sess-team-template-general"
	t.Logf("  Waiting for editor pod %s...", editorPod)
	waitForPodRunning(t, editorPod, 120*time.Second)
	time.Sleep(5 * time.Second) // Wait for overlay composition

	// Step 2: Install vim inside the editor pod
	t.Log("  Installing vim in editor pod...")
	installCmd := exec.Command("kubectl", "exec", "-n", smokeNamespace, editorPod, "--",
		"chroot", "/sandbox/rootfs", "sh", "-c",
		"apt-get update -qq && apt-get install -y -qq vim </dev/null")
	output, err := installCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install vim in editor: %v\nOutput: %s", err, string(output))
	}
	t.Log("  vim installed successfully")

	// Verify vim is there
	assertCommandPresent(t, editorPod, "vim", "vim installed in editor pod")

	// Step 3: Save team template
	t.Log("  Saving team template...")
	resp = doPost(t, baseURL+"/api/team/template/save", nil, token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		t.Fatalf("save team template: %d %s", resp.StatusCode, string(respBody))
	}
	t.Log("  Team template saved successfully")
}

// ---------------------------------------------------------------------------
// Reset @base Sentinel
// ---------------------------------------------------------------------------

func resetBaseSentinel(t *testing.T, ctx context.Context, platformDSN string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, platformDSN)
	if err != nil {
		t.Fatalf("connect to platform DB: %v", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, `UPDATE sandbox_templates SET top_layer_id = '@base' WHERE slug = 'base' AND scope = 'global'`)
	if err != nil {
		t.Fatalf("reset @base sentinel: %v", err)
	}

	// Verify the update took effect
	var topLayerID string
	err = conn.QueryRow(ctx, `SELECT top_layer_id FROM sandbox_templates WHERE slug = 'base' AND scope = 'global'`).Scan(&topLayerID)
	if err != nil {
		t.Fatalf("verify reset: %v", err)
	}
	if topLayerID != "@base" {
		t.Fatalf("reset failed: top_layer_id=%q, want '@base'", topLayerID)
	}
	t.Log("  Reset @base.top_layer_id to sentinel '@base'")
}

// ---------------------------------------------------------------------------
// HTTP Helpers
// ---------------------------------------------------------------------------

func doPost(t *testing.T, url string, body any, token string) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest("POST", url, bodyReader)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Astonish-Team", "general")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request %s: %v", url, err)
	}
	return resp
}

// ---------------------------------------------------------------------------
// Platform Provider Seeding
// ---------------------------------------------------------------------------

func seedPlatformProvider(t *testing.T, ctx context.Context, pgStore *pgstore.PGStore) {
	t.Helper()

	bifrostURL := os.Getenv("BIFROST_BASE_URL")
	if bifrostURL == "" {
		bifrostURL = "https://bifrost.local.muxpie.com"
	}

	apiKey := os.Getenv("BIFROST_API_KEY")
	if apiKey == "" {
		t.Fatal("BIFROST_API_KEY env var is required for the smoke test (LLM provider)")
	}

	settings := &store.PlatformSettings{
		DefaultProvider: "Bifrost",
		DefaultModel:    "sapaicore/anthropic--claude-4.6-opus",
		Providers: map[string]store.ProviderConfig{
			"Bifrost": {
				"type":     "openai_compat",
				"base_url": bifrostURL,
				"api_key":  apiKey,
			},
		},
	}

	if err := pgStore.PlatformSettings().Save(ctx, settings); err != nil {
		t.Fatalf("seed platform provider: %v", err)
	}
	t.Log("Seeded Bifrost provider in platform settings")
}
