package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/cache"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

func testSetup(t *testing.T) func() {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "api-cache-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	cache.SetCacheDir(tmpDir)

	return func() {
		os.RemoveAll(tmpDir)
		cache.SetCacheDir("")
	}
}

func TestMCPStatusHandler(t *testing.T) {
	cleanup := testSetup(t)
	defer cleanup()

	// Without platform context (no MCP store), handler returns empty servers list
	req, err := http.NewRequest("GET", "/api/mcp/status", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(MCPStatusHandler)
	handler.ServeHTTP(rr, req)

	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response struct {
		Servers []cache.ServerStatus `json:"servers"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response.Servers) != 0 {
		t.Errorf("expected 0 servers without platform context, got %d", len(response.Servers))
	}
}

func TestMCPInspectorUsesSandbox(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		cfg  config.MCPServerConfig
		want bool
	}{
		{"default stdio uses sandbox", config.MCPServerConfig{Command: "npx"}, true},
		{"explicit stdio uses sandbox", config.MCPServerConfig{Transport: "stdio", Command: "npx"}, true},
		{"sse stays remote", config.MCPServerConfig{Transport: "sse", URL: "https://example.test/sse"}, false},
		{"streamable http stays remote", config.MCPServerConfig{Transport: "streamable-http", URL: "https://example.test/mcp"}, false},
		{"url without transport still uses sandbox", config.MCPServerConfig{URL: "https://example.test/sse"}, true},
		{"stdio with url still uses sandbox", config.MCPServerConfig{Transport: "stdio", Command: "npx", URL: "https://example.test/sse"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := mcpInspectorUsesSandbox(tt.cfg); got != tt.want {
				t.Fatalf("mcpInspectorUsesSandbox() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDecodeMCPToolSchemas(t *testing.T) {
	t.Parallel()
	tools, err := decodeMCPToolSchemas(json.RawMessage(`[{"name":"lookup","description":"Lookup docs","inputSchema":{"type":"object","properties":{"q":{"type":"string"}}}}]`))
	if err != nil {
		t.Fatalf("decodeMCPToolSchemas failed: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "lookup" || tools[0].Description != "Lookup docs" {
		t.Fatalf("unexpected tool metadata: %+v", tools[0])
	}
	params, ok := tools[0].Parameters.(map[string]any)
	if !ok || params["type"] != "object" {
		t.Fatalf("expected object parameters, got %#v", tools[0].Parameters)
	}
}

func TestLogMCPInspectorFailure_IncludesListToolsDiagnostics(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	cfg := config.MCPServerConfig{
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp"},
		Env: map[string]string{
			"CONTEXT7_API_KEY": "secret-value",
		},
	}
	stderr := bytes.NewBufferString("server exited during initialize")
	cause := errors.New("calling initialize: EOF")
	err := newMCPSandboxDiagnosticsError("failed to list tools", cause, stderr, bytes.NewBufferString("npm ERR! network timeout secret-value"), cfg)

	logMCPInspectorFailure("MCP inspector failed to list tools", "context7", cfg, err, stderr, "phase", "list_tools")

	out := buf.String()
	for _, want := range []string{"MCP inspector failed to list tools", "server=context7", "phase=list_tools", "command=npx", "@upstash/context7-mcp", "CONTEXT7_API_KEY", "calling initialize: EOF", "server exited during initialize", "npm ERR! network timeout"} {
		if !strings.Contains(out, want) {
			t.Fatalf("expected log output to contain %q, got %s", want, out)
		}
	}
	if strings.Contains(out, "secret-value") {
		t.Fatalf("log output leaked env value: %s", out)
	}
}

func TestListServerToolsHandler_Error(t *testing.T) {
	cleanup := testSetup(t)
	defer cleanup()

	// Invalidate cache to ensure it tries to load
	cache.InvalidateCache()

	// Create a request with a non-existent server
	req, err := http.NewRequest("GET", "/api/mcp/nonexistent/tools", nil)
	if err != nil {
		t.Fatal(err)
	}
	req = mux.SetURLVars(req, map[string]string{"serverName": "nonexistent"})

	// Record the response
	rr := httptest.NewRecorder()
	handler := http.HandlerFunc(ListServerToolsHandler)
	handler.ServeHTTP(rr, req)

	// Verify results
	if status := rr.Code; status != http.StatusOK {
		t.Errorf("handler returned wrong status code: got %v want %v", status, http.StatusOK)
	}

	var response ListServerToolsResponse
	if err := json.NewDecoder(rr.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Error == "" {
		t.Error("expected error message for non-existent server, got empty")
	}
}

// Any access is a test failure, not a usable config. In particular refresh cannot
// reach async discovery even if authorization regresses. No external MCP process
// or network endpoint is reachable from this fixture.
type mcpRejectedStore struct {
	store.MCPServerStore
	t     *testing.T
	calls int
}

func (s *mcpRejectedStore) touched() {
	s.calls++
	s.t.Fatal("rejected request accessed MCP persistence before authorization")
}
func (s *mcpRejectedStore) Get(context.Context, string) (*store.MCPServer, error) {
	s.touched()
	return nil, nil
}
func (s *mcpRejectedStore) List(context.Context) ([]store.MCPServer, error) {
	s.touched()
	return nil, nil
}
func (s *mcpRejectedStore) Save(context.Context, *store.MCPServer) error {
	s.touched()
	return nil
}
func (s *mcpRejectedStore) Delete(context.Context, string) error {
	s.touched()
	return nil
}
func (s *mcpRejectedStore) UpdateCachedTools(context.Context, string, json.RawMessage) error {
	s.touched()
	return nil
}

func TestMCPOrgAdminScopedMutationLifecycle(t *testing.T) {
	standardServerLocalInstallFixture(t)
	const name = "urgent-org-admin"
	if _, loaded := discoveryInFlight.LoadOrStore(name, struct{}{}); loaded {
		t.Fatal("unexpected discovery fixture collision")
	}
	t.Cleanup(func() { discoveryInFlight.Delete(name) })
	for _, scope := range []string{"", "team", "org"} {
		scopeName := scope
		if scopeName == "" {
			scopeName = "default_team"
		}
		t.Run(scopeName, func(t *testing.T) {
			_, _, unchanged := standardServerSecretStateFixture(t, false, false)
			team, org, platform := newMockMCPStore(), newMockMCPStore(), newMockMCPStore()
			svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform}
			selected := team
			if scope == "org" {
				selected = org
			}
			for _, operation := range []struct {
				name, method, body string
				handler            http.HandlerFunc
				want               int
			}{
				{"create", http.MethodPost, `{"name":"urgent-org-admin","transport":"sse","url":"https://example.invalid/mcp","env":{"TOKEN":"scoped-secret"}}`, CreateMCPPlatformServerHandler, http.StatusCreated},
				{"update", http.MethodPut, `{"env":{"TOKEN":"scoped-secret"},"url":"https://example.invalid/updated"}`, UpdateMCPPlatformServerHandler, http.StatusOK},
				{"toggle", http.MethodPatch, `{"enabled":false}`, ToggleMCPPlatformServerHandler, http.StatusOK},
				{"delete", http.MethodDelete, ``, DeleteMCPPlatformServerHandler, http.StatusOK},
				{"inline_install", http.MethodPost, `{"serverName":"urgent-org-admin","config":{"transport":"sse","url":"https://example.invalid/inline","env":{"TOKEN":"scoped-secret"}}}`, InstallInlineMCPServerHandler, http.StatusOK},
				{"legacy_settings", http.MethodPut, `{"mcpServers":{"urgent-org-admin":{"transport":"sse","url":"https://example.invalid/legacy","env":{"TOKEN":"scoped-secret"}}}}`, UpdateMCPSettingsHandler, http.StatusOK},
			} {
				t.Run(operation.name, func(t *testing.T) {
					r := httptest.NewRequest(operation.method, "/?scope="+scope, strings.NewReader(operation.body))
					r = mux.SetURLVars(r, map[string]string{"name": name})
					// Org admin only: no platform role or team membership shortcut.
					r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "org-admin", Role: "admin"}))
					w := httptest.NewRecorder()
					operation.handler(w, r)
					if w.Code != operation.want {
						t.Fatalf("status=%d want=%d", w.Code, operation.want)
					}
					if strings.Contains(w.Body.String(), "scoped-secret") {
						t.Error("response exposed scoped secret")
					}
					got := selected.servers[name]
					if operation.method == http.MethodDelete {
						if got != nil {
							t.Error("delete left selected row")
						}
					} else if got == nil || got.Env["TOKEN"] != "scoped-secret" || got.CreatedBy != "org-admin" {
						t.Error("mutation did not retain authenticated scoped configuration")
					} else {
						switch operation.name {
						case "update":
							if got.URL != "https://example.invalid/updated" {
								t.Error("update not persisted")
							}
						case "toggle":
							if got.Enabled == nil || *got.Enabled {
								t.Error("toggle not persisted")
							}
						case "inline_install":
							if got.URL != "https://example.invalid/inline" {
								t.Error("inline install not persisted")
							}
						case "legacy_settings":
							if got.URL != "https://example.invalid/legacy" {
								t.Error("legacy settings not persisted")
							}
						}
					}
					for _, other := range []*mockMCPServerStore{team, org, platform} {
						if other != selected && (len(other.servers) != 0 || len(other.deleted) != 0) {
							t.Error("mutation crossed scope")
						}
					}
					unchanged()
				})
			}
		})
	}
}

type mcpRejectedTransport struct{ calls int }

func (s *mcpRejectedTransport) RoundTrip(*http.Request) (*http.Response, error) {
	s.calls++
	return nil, errors.New("external transport disabled by MCP rejection fixture")
}

// mcpGlobalConfigFileFixture detects both a legacy file fallback and a global
// app-config rewrite. Noncanonical JSON/YAML keeps a same-value save observable.
func mcpGlobalConfigFileFixture(t *testing.T) func(*testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	t.Setenv("SUDO_USER", "")
	configDir, err := config.GetConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"config.yaml":     "# global file must not be rewritten\nsandbox:\n  enabled: false\n",
		"mcp_config.json": "{\"mcpServers\": {\"global-file-sentinel\": {\"transport\": \"sse\", \"url\": \"https://example.invalid/global\"}}}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(configDir, name), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return func(t *testing.T) {
		t.Helper()
		for name, content := range files {
			got, err := os.ReadFile(filepath.Join(configDir, name))
			if err != nil || string(got) != content {
				t.Errorf("global %s changed: read error=%v", name, err)
			}
		}
	}
}

// Count reads as well as writes: reading a broader scope can leak configuration
// even if the eventual save happens to be rejected.
type mcpScopedEffectStore struct {
	*mockMCPServerStore
	gets, lists, saves, deletes, discoveries int
}

func (s *mcpScopedEffectStore) Get(ctx context.Context, name string) (*store.MCPServer, error) {
	s.gets++
	return s.mockMCPServerStore.Get(ctx, name)
}
func (s *mcpScopedEffectStore) List(ctx context.Context) ([]store.MCPServer, error) {
	s.lists++
	return s.mockMCPServerStore.List(ctx)
}
func (s *mcpScopedEffectStore) Save(ctx context.Context, server *store.MCPServer) error {
	s.saves++
	return s.mockMCPServerStore.Save(ctx, server)
}
func (s *mcpScopedEffectStore) Delete(ctx context.Context, name string) error {
	s.deletes++
	return s.mockMCPServerStore.Delete(ctx, name)
}
func (s *mcpScopedEffectStore) UpdateCachedTools(context.Context, string, json.RawMessage) error {
	s.discoveries++
	return nil
}

func TestMCPRemainingMutationAuthorizedScope(t *testing.T) {
	unchangedFiles := mcpGlobalConfigFileFixture(t)
	const name = "remaining-scoped-server"
	// Exercise authorization, lookup and persistence, not an external MCP server.
	// The existing in-flight gate prevents any discovery goroutine/process start.
	if _, loaded := discoveryInFlight.LoadOrStore(name, struct{}{}); loaded {
		t.Fatal("unexpected discovery fixture collision")
	}
	t.Cleanup(func() { discoveryInFlight.Delete(name) })
	for _, operation := range []struct {
		name, method, body       string
		handler                  http.HandlerFunc
		want, gets, lists, saves int
	}{
		{"refresh", http.MethodPost, "", RefreshMCPPlatformServerHandler, http.StatusAccepted, 1, 0, 0},
		{"legacy_refresh", http.MethodPost, "", RefreshMCPServerHandler, http.StatusOK, 1, 0, 0},
		{"legacy_patch", http.MethodPatch, `{"enabled":false}`, UpdateMCPServerHandler, http.StatusOK, 1, 0, 1},
		{"platform_patch", http.MethodPatch, `{"enabled":false}`, ToggleMCPPlatformServerHandler, http.StatusOK, 1, 0, 1},
		{"inline_install", http.MethodPost, `{"serverName":"remaining-scoped-server","config":{"transport":"sse","url":"https://example.invalid/new","env":{"TOKEN":"replacement"}}}`, InstallInlineMCPServerHandler, http.StatusOK, 0, 0, 1},
		{"legacy_settings", http.MethodPut, `{"mcpServers":{"remaining-scoped-server":{"transport":"sse","url":"https://example.invalid/new","env":{"TOKEN":"replacement"}}}}`, UpdateMCPSettingsHandler, http.StatusOK, 0, 1, 1},
	} {
		for _, scope := range []string{"", string(mcpScopeTeam), string(mcpScopeOrg), string(mcpScopePlatform)} {
			scopeName := scope
			if scopeName == "" {
				scopeName = "default_team"
			}
			t.Run(operation.name+"/"+scopeName, func(t *testing.T) {
				t.Cleanup(func() { unchangedFiles(t) })
				_, _, unchangedSecrets := standardServerSecretStateFixture(t, true, true)
				t.Cleanup(unchangedSecrets)
				stores := make(map[mcpWriteScope]*mcpScopedEffectStore)
				for _, key := range []mcpWriteScope{mcpScopeTeam, mcpScopeOrg, mcpScopePlatform} {
					stores[key] = &mcpScopedEffectStore{mockMCPServerStore: newMockMCPStore(&store.MCPServer{
						Name: name, Transport: "sse", URL: "https://example.invalid/" + string(key),
						Env: map[string]string{"TOKEN": string(key) + "-secret"}, CachedTools: json.RawMessage(`[{"name":"cached"}]`),
					})}
				}
				selectedScope := mcpWriteScope(scope)
				if selectedScope == "" {
					selectedScope = mcpScopeTeam
				}
				selected := stores[selectedScope]
				user := &PlatformUser{ID: "org-admin", Role: "admin"}
				if selectedScope == mcpScopePlatform {
					// Platform authorization must not require an unrelated org role.
					user = &PlatformUser{ID: "platform-admin", Role: "member", PlatformRole: "superadmin"}
				}
				svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: stores[mcpScopeTeam], MCPServers: stores[mcpScopeOrg], PlatformMCPServers: stores[mcpScopePlatform]}
				r := httptest.NewRequest(operation.method, "/?scope="+scope, strings.NewReader(operation.body))
				r = mux.SetURLVars(r, map[string]string{"name": name})
				r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), user))
				w := httptest.NewRecorder()
				operation.handler(w, r)
				if w.Code != operation.want {
					t.Errorf("status=%d want=%d body=%s", w.Code, operation.want, w.Body.String())
				}
				if selected.gets != operation.gets || selected.lists != operation.lists || selected.saves != operation.saves {
					t.Errorf("selected scope calls: get=%d list=%d save=%d; want %d/%d/%d", selected.gets, selected.lists, selected.saves, operation.gets, operation.lists, operation.saves)
				}
				for key, target := range stores {
					if target != selected && (target.gets+target.lists+target.saves+target.deletes != 0) {
						t.Errorf("operation accessed unrelated %s scope", key)
					}
					if target.discoveries != 0 {
						t.Error("fixture unexpectedly allowed discovery cache mutation")
					}
					got := target.servers[name]
					wantSecret := string(key) + "-secret"
					if target == selected && (operation.name == "inline_install" || operation.name == "legacy_settings") {
						wantSecret = "replacement"
					}
					if got.Env["TOKEN"] != wantSecret {
						t.Errorf("%s scope secret unexpectedly changed", key)
					}
					if target == selected && (operation.name == "legacy_patch" || operation.name == "platform_patch") {
						if got.Enabled == nil || *got.Enabled {
							t.Error("selected scope was not disabled")
						}
					} else if got.Enabled != nil {
						t.Errorf("%s scope enabled state unexpectedly changed", key)
					}
				}
				if strings.Contains(w.Body.String(), "-secret") || strings.Contains(w.Body.String(), "replacement") {
					t.Error("response leaked an MCP secret")
				}
			})
		}
	}
}

// Valid JSON is intentional: the unread-body tripwire above proves ordering,
// while this regression proves an actual forbidden write cannot be hidden by
// the decoder rejecting an otherwise invalid request first.
func TestMCPLegacyPatchRejectedScopeDoesNotMutate(t *testing.T) {
	unchangedFiles := mcpGlobalConfigFileFixture(t)
	for _, tc := range []struct {
		name, scope string
		missing     bool
		want        int
	}{
		{"org_admin_platform", "platform", false, http.StatusForbidden},
		{"effective", "effective", false, http.StatusBadRequest},
		{"unknown", "typo", false, http.StatusBadRequest},
		{"missing_default_team", "", true, http.StatusServiceUnavailable},
		{"missing_team", "team", true, http.StatusServiceUnavailable},
		{"missing_org", "org", true, http.StatusServiceUnavailable},
		{"missing_platform", "platform", true, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(func() { unchangedFiles(t) })
			_, _, unchangedSecrets := standardServerSecretStateFixture(t, true, true)
			t.Cleanup(unchangedSecrets)
			const name = "legacy-patch-forbidden"
			newStore := func() *mcpScopedEffectStore {
				return &mcpScopedEffectStore{mockMCPServerStore: newMockMCPStore(&store.MCPServer{
					Name: name, Transport: "sse", URL: "https://example.invalid/mcp", Env: map[string]string{"TOKEN": "unchanged-secret"},
				})}
			}
			team, org, platform := newStore(), newStore(), newStore()
			svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform}
			if tc.missing {
				switch mcpWriteScope(tc.scope) {
				case "", mcpScopeTeam:
					svc.TeamMCPServers = nil
				case mcpScopeOrg:
					svc.MCPServers = nil
				case mcpScopePlatform:
					svc.PlatformMCPServers = nil
				}
			}
			user := &PlatformUser{ID: "org-admin", Role: "admin"}
			if tc.missing {
				user.PlatformRole = "superadmin"
			}
			r := httptest.NewRequest(http.MethodPatch, "/api/mcp/servers/"+name+"?scope="+tc.scope, strings.NewReader(`{"enabled":false}`))
			r = mux.SetURLVars(r, map[string]string{"name": name})
			r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), user))
			w := httptest.NewRecorder()
			UpdateMCPServerHandler(w, r)
			if w.Code != tc.want {
				t.Errorf("status=%d want=%d", w.Code, tc.want)
			}
			for scope, target := range map[string]*mcpScopedEffectStore{"team": team, "org": org, "platform": platform} {
				if target.gets+target.lists+target.saves+target.deletes+target.discoveries != 0 {
					t.Errorf("rejected PATCH accessed %s persistence", scope)
				}
				if got := target.servers[name]; got.Enabled != nil || got.Env["TOKEN"] != "unchanged-secret" {
					t.Errorf("rejected PATCH mutated %s configuration", scope)
				}
			}
			if _, started := discoveryInFlight.Load(name); started {
				t.Error("rejected PATCH started discovery")
			}
		})
	}
}

func TestMCPPlatformMissingRowHasNoMutationEffects(t *testing.T) {
	unchangedFiles := mcpGlobalConfigFileFixture(t)
	for _, operation := range []struct {
		name, method, suffix, body string
		handler                    http.HandlerFunc
	}{
		{"toggle", http.MethodPatch, "", `{"enabled":false}`, ToggleMCPPlatformServerHandler},
		{"refresh", http.MethodPost, "/refresh", "", RefreshMCPPlatformServerHandler},
	} {
		for _, scope := range []string{"", string(mcpScopeTeam), string(mcpScopeOrg), string(mcpScopePlatform)} {
			scopeName := scope
			if scopeName == "" {
				scopeName = "default_team"
			}
			t.Run(operation.name+"/"+scopeName, func(t *testing.T) {
				t.Cleanup(func() { unchangedFiles(t) })
				name := "missing-platform-" + operation.name + "-" + scopeName
				selectedScope := mcpWriteScope(scope)
				if selectedScope == "" {
					selectedScope = mcpScopeTeam
				}
				stores := make(map[mcpWriteScope]*mcpScopedEffectStore)
				for _, key := range []mcpWriteScope{mcpScopeTeam, mcpScopeOrg, mcpScopePlatform} {
					stores[key] = &mcpScopedEffectStore{mockMCPServerStore: newMockMCPStore()}
					if key != selectedScope {
						// A same-name row elsewhere must not become a write fallback.
						stores[key].servers[name] = &store.MCPServer{Name: name, Env: map[string]string{"TOKEN": "unrelated-secret"}}
					}
				}
				// The selected store returns (nil, nil), matching a missing DB row.
				user := &PlatformUser{ID: "org-admin", Role: "admin"}
				if selectedScope == mcpScopePlatform {
					user = &PlatformUser{ID: "platform-admin", Role: "member", PlatformRole: "superadmin"}
				}
				svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: stores[mcpScopeTeam], MCPServers: stores[mcpScopeOrg], PlatformMCPServers: stores[mcpScopePlatform]}
				r := httptest.NewRequest(operation.method, "/api/mcp-platform/servers/"+name+operation.suffix+"?scope="+scope, strings.NewReader(operation.body))
				r = mux.SetURLVars(r, map[string]string{"name": name})
				r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), user))
				w := httptest.NewRecorder()
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							t.Errorf("missing row panicked: %v", recovered)
						}
					}()
					operation.handler(w, r)
				}()
				if w.Code != http.StatusNotFound {
					t.Errorf("status=%d want=%d body=%s", w.Code, http.StatusNotFound, w.Body.String())
				}
				var response struct {
					Error string `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error != "MCP server not found: "+name {
					t.Errorf("expected existing not-found JSON convention, got %q (decode error=%v)", w.Body.String(), err)
				}
				for key, target := range stores {
					wantGets := 0
					if key == selectedScope {
						wantGets = 1
					}
					if target.gets != wantGets || target.lists+target.saves+target.deletes+target.discoveries != 0 {
						t.Errorf("%s calls: get=%d want=%d list=%d save=%d delete=%d discovery=%d", key, target.gets, wantGets, target.lists, target.saves, target.deletes, target.discoveries)
					}
					if key == selectedScope {
						if len(target.servers) != 0 {
							t.Error("missing row was created")
						}
					} else if got := target.servers[name]; got == nil || got.Enabled != nil || got.Env["TOKEN"] != "unrelated-secret" {
						t.Errorf("unrelated %s row changed", key)
					}
				}
				if _, started := discoveryInFlight.Load(name); started {
					t.Error("missing row started discovery")
				}
			})
		}
	}
}

type mcpGetResultStore struct {
	mcpRejectedStore
	server *store.MCPServer
	err    error
	names  []string
}

func (s *mcpGetResultStore) Get(_ context.Context, name string) (*store.MCPServer, error) {
	s.names = append(s.names, name)
	return s.server, s.err
}

func TestGetMCPPlatformServerHandlerLookup(t *testing.T) {
	for _, scope := range []string{"", string(mcpScopeTeam), string(mcpScopeOrg), string(mcpScopePlatform), "effective", "unknown"} {
		scopeName := scope
		if scopeName == "" {
			scopeName = "default_team"
		}
		for _, result := range []struct {
			name       string
			missingRow bool
			noStore    bool
			err        error
			want       int
		}{
			{name: "missing_row", missingRow: true, want: http.StatusNotFound},
			{name: "storage_error", missingRow: true, err: errors.New("private-storage-error"), want: http.StatusNotFound},
			{name: "row_and_error", err: errors.New("private-storage-error"), want: http.StatusNotFound},
			{name: "populated", want: http.StatusOK},
			{name: "missing_store", noStore: true, want: http.StatusServiceUnavailable},
		} {
			t.Run(scopeName+"/"+result.name, func(t *testing.T) {
				const name = "get-regression"
				const rawURL = "https://example.invalid/mcp?token=url-secret#fragment-secret"
				stores := make(map[mcpWriteScope]*mcpGetResultStore)
				for _, key := range []mcpWriteScope{mcpScopeTeam, mcpScopeOrg, mcpScopePlatform} {
					stores[key] = &mcpGetResultStore{
						mcpRejectedStore: mcpRejectedStore{t: t},
						server: &store.MCPServer{
							Name: name, Transport: "sse", URL: rawURL,
							Env:         map[string]string{"VALUE": string(key) + "-secret", "REF": "{{CREDENTIAL:test:token}}", "EMPTY": ""},
							CachedTools: json.RawMessage(`[{"name":"cached"}]`),
						},
					}
				}
				// Preserve the existing GET resolver: default, effective and unknown
				// scopes select team; reads do not acquire write authorization.
				selectedScope := mcpWriteScope(scope)
				if selectedScope != mcpScopeOrg && selectedScope != mcpScopePlatform {
					selectedScope = mcpScopeTeam
				}
				selected := stores[selectedScope]
				selected.err = result.err
				if result.missingRow {
					selected.server = nil
				}
				svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: stores[mcpScopeTeam], MCPServers: stores[mcpScopeOrg], PlatformMCPServers: stores[mcpScopePlatform]}
				if result.noStore {
					switch selectedScope {
					case mcpScopeTeam:
						svc.TeamMCPServers = nil
					case mcpScopeOrg:
						svc.MCPServers = nil
					case mcpScopePlatform:
						svc.PlatformMCPServers = nil
					}
				}
				r := httptest.NewRequest(http.MethodGet, "/api/mcp-platform/servers/"+name+"?scope="+scope, nil)
				r = mux.SetURLVars(r, map[string]string{"name": name})
				r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "reader", Role: "member"}))
				w := httptest.NewRecorder()
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							t.Errorf("GET panicked: %v", recovered)
						}
					}()
					GetMCPPlatformServerHandler(w, r)
				}()
				if w.Code != result.want {
					t.Errorf("status=%d want=%d body=%s", w.Code, result.want, w.Body.String())
				}
				if result.want == http.StatusOK {
					var item MCPServerListItem
					if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
						t.Fatal(err)
					}
					if item.Name != name || item.Scope != scope || item.Transport != "sse" || !item.Enabled || !item.HasTools || item.ToolCount != 1 || string(item.CachedTools) != `[{"name":"cached"}]` {
						t.Errorf("GET response metadata changed: %+v", item)
					}
					if item.Env["VALUE"] != "****" || item.Env["REF"] != "{{CREDENTIAL:test:token}}" || item.Env["EMPTY"] != "" || item.URL != "https://example.invalid/mcp?token=%2A%2A%2A%2A#****" {
						t.Errorf("GET redaction changed: %+v", item)
					}
				} else {
					var response struct {
						Error string `json:"error"`
					}
					wantError := "MCP server not found: " + name
					if result.noStore {
						wantError = "MCP server store not available for scope: " + scope
					}
					if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil || response.Error != wantError {
						t.Errorf("error response=%q want=%q decode=%v", w.Body.String(), wantError, err)
					}
				}
				if strings.Contains(w.Body.String(), "-secret") || strings.Contains(w.Body.String(), "private-storage-error") {
					t.Error("GET leaked a secret or storage error")
				}
				for key, target := range stores {
					wantGets := 0
					if target == selected && !result.noStore {
						wantGets = 1
					}
					if len(target.names) != wantGets || (wantGets == 1 && target.names[0] != name) || target.calls != 0 {
						t.Errorf("unexpected %s store access: names=%v other=%d", key, target.names, target.calls)
					}
					if target.server != nil && (target.server.Env["VALUE"] != string(key)+"-secret" || target.server.URL != rawURL) {
						t.Errorf("GET mutated %s runtime configuration", key)
					}
				}
			})
		}
	}
}

func TestMCPMutationScopeRejectionHasNoStartEffects(t *testing.T) {
	unchangedFiles := mcpGlobalConfigFileFixture(t)
	transport := &mcpRejectedTransport{}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previousTransport })
	// Extends the existing platform-only status checks: cover every denied scope
	// and missing destination using tripwires before configuration/discovery.
	for _, operation := range []struct {
		name, method string
		handler      http.HandlerFunc
	}{
		{"create", http.MethodPost, CreateMCPPlatformServerHandler},
		{"update", http.MethodPut, UpdateMCPPlatformServerHandler},
		{"delete", http.MethodDelete, DeleteMCPPlatformServerHandler},
		{"toggle", http.MethodPatch, ToggleMCPPlatformServerHandler},
		{"legacy_patch", http.MethodPatch, UpdateMCPServerHandler},
		{"refresh_discovery", http.MethodPost, RefreshMCPPlatformServerHandler},
		{"legacy_refresh_discovery", http.MethodPost, RefreshMCPServerHandler},
		{"legacy_settings", http.MethodPut, UpdateMCPSettingsHandler},
		{"inline_install", http.MethodPost, InstallInlineMCPServerHandler},
		{"internet_install", http.MethodPost, InternetMCPInstallHandler},
		{"catalog_install", http.MethodPost, InstallMCPStoreServerHandler},
	} {
		for _, tc := range []struct {
			name, scope, role, platformRole string
			missing                         bool
			want                            int
		}{
			{"member_default_team", "", "member", "", false, http.StatusForbidden},
			{"member_team", "team", "member", "", false, http.StatusForbidden},
			{"member_org", "org", "member", "", false, http.StatusForbidden},
			{"member_platform", "platform", "member", "", false, http.StatusForbidden},
			{"org_admin_platform", "platform", "admin", "", false, http.StatusForbidden},
			{"effective_scope", "effective", "admin", "superadmin", false, http.StatusBadRequest},
			{"unknown_scope", "typo", "admin", "superadmin", false, http.StatusBadRequest},
			{"missing_default_team", "", "admin", "superadmin", true, http.StatusServiceUnavailable},
			{"missing_team", "team", "admin", "superadmin", true, http.StatusServiceUnavailable},
			{"missing_org", "org", "admin", "superadmin", true, http.StatusServiceUnavailable},
			{"missing_platform", "platform", "admin", "superadmin", true, http.StatusServiceUnavailable},
		} {
			t.Run(operation.name+"/"+tc.name, func(t *testing.T) {
				t.Cleanup(func() { unchangedFiles(t) })
				_, _, unchanged := standardServerSecretStateFixture(t, false, false)
				t.Cleanup(unchanged)
				team, org, platform := &mcpRejectedStore{t: t}, &mcpRejectedStore{t: t}, &mcpRejectedStore{t: t}
				settings := &standardServerEffectSettings{}
				svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform, Settings: settings}
				if tc.missing {
					switch tc.scope {
					case "", "team":
						svc.TeamMCPServers = nil
					case "org":
						svc.MCPServers = nil
					case "platform":
						svc.PlatformMCPServers = nil
					}
				}
				body := &standardServerUnreadBody{}
				r := httptest.NewRequest(operation.method, "/?scope="+tc.scope, body)
				r = mux.SetURLVars(r, map[string]string{"name": "urgent-no-start", "id": "urgent-no-start"})
				r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "actor", Role: tc.role, PlatformRole: tc.platformRole}))
				w := httptest.NewRecorder()
				operation.handler(w, r)
				if w.Code != tc.want {
					t.Errorf("status=%d want=%d", w.Code, tc.want)
				}
				if body.reads != 0 || team.calls != 0 || org.calls != 0 || platform.calls != 0 || settings.saves != 0 {
					t.Error("rejected request crossed a body/persistence/settings boundary")
				}
				if transport.calls != 0 {
					t.Error("rejected request attempted catalog/discovery network access")
				}
				if _, started := discoveryInFlight.Load("urgent-no-start"); started {
					t.Error("rejected request started discovery")
				}
				unchanged()
			})
		}
	}
}
