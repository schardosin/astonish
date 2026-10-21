package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/cache"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/credentials"
	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

// --- Mock MCPServerStore ---

type mockMCPServerStore struct {
	servers map[string]*store.MCPServer
	deleted []string
}

func newMockMCPStore(servers ...*store.MCPServer) *mockMCPServerStore {
	m := &mockMCPServerStore{servers: make(map[string]*store.MCPServer)}
	for _, s := range servers {
		m.servers[s.Name] = s
	}
	return m
}

func (m *mockMCPServerStore) List(_ context.Context) ([]store.MCPServer, error) {
	var result []store.MCPServer
	for _, s := range m.servers {
		result = append(result, *s)
	}
	return result, nil
}

func (m *mockMCPServerStore) Get(_ context.Context, name string) (*store.MCPServer, error) {
	s, ok := m.servers[name]
	if !ok {
		return nil, nil
	}
	return s, nil
}

func (m *mockMCPServerStore) Save(_ context.Context, server *store.MCPServer) error {
	m.servers[server.Name] = server
	return nil
}

func (m *mockMCPServerStore) Delete(_ context.Context, name string) error {
	delete(m.servers, name)
	m.deleted = append(m.deleted, name)
	return nil
}

func (m *mockMCPServerStore) UpdateCachedTools(_ context.Context, name string, tools json.RawMessage) error {
	if s, ok := m.servers[name]; ok {
		s.CachedTools = tools
	}
	return nil
}

// --- Mock platformSecrets ---

type mockPlatformSecrets struct {
	secrets map[string]string
	removed []string
	written []string
}

func newMockPlatformSecrets() *mockPlatformSecrets {
	return &mockPlatformSecrets{secrets: make(map[string]string)}
}

func (m *mockPlatformSecrets) GetSecret(key string) string {
	return m.secrets[key]
}

func (m *mockPlatformSecrets) SetSecret(key, value string) error {
	m.written = append(m.written, key)
	m.secrets[key] = value
	return nil
}

func (m *mockPlatformSecrets) RemoveSecret(key string) error {
	delete(m.secrets, key)
	m.removed = append(m.removed, key)
	return nil
}

// --- Helper: create request with platform services context ---

func platformRequest(t *testing.T, method, url string, mcpStore store.MCPServerStore) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, url, nil)
	svc := &store.Services{
		Mode:       store.ModePlatform,
		MCPServers: mcpStore, // org-level (default scope)
	}
	ctx := store.WithServices(r.Context(), svc)
	return r.WithContext(ctx)
}

func platformRequestWithScopes(t *testing.T, method, url string, org, team, platform store.MCPServerStore) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, url, nil)
	svc := &store.Services{
		Mode:               store.ModePlatform,
		MCPServers:         org,
		TeamMCPServers:     team,
		PlatformMCPServers: platform,
	}
	ctx := store.WithServices(r.Context(), svc)
	return r.WithContext(ctx)
}

// =============================================================================
// MCPStatusHandler tests
// =============================================================================

func TestMCPStatusHandler_HealthyWhenToolsExist(t *testing.T) {
	tools := json.RawMessage(`[{"name":"resolve","description":"Resolves a library ID"}]`)
	mcpStore := newMockMCPStore(&store.MCPServer{
		Name:        "context7",
		Command:     "npx",
		Transport:   "stdio",
		CachedTools: tools,
	})

	r := platformRequest(t, "GET", "/api/mcp/status", mcpStore)
	rr := httptest.NewRecorder()
	MCPStatusHandler(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp MCPStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(resp.Servers))
	}
	if resp.Servers[0].Status != "healthy" {
		t.Errorf("expected status 'healthy', got %q", resp.Servers[0].Status)
	}
	if resp.Servers[0].ToolCount != 1 {
		t.Errorf("expected tool_count 1, got %d", resp.Servers[0].ToolCount)
	}
}

func TestMCPStatusHandler_ConfiguredWhenNoTools(t *testing.T) {
	mcpStore := newMockMCPStore(&store.MCPServer{
		Name:      "context7",
		Command:   "npx",
		Transport: "stdio",
		// No CachedTools
	})

	r := platformRequest(t, "GET", "/api/mcp/status", mcpStore)
	rr := httptest.NewRecorder()
	MCPStatusHandler(rr, r)

	var resp MCPStatusResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if len(resp.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(resp.Servers))
	}
	if resp.Servers[0].Status != "configured" {
		t.Errorf("expected status 'configured', got %q", resp.Servers[0].Status)
	}
}

func TestMCPStatusHandler_Disabled(t *testing.T) {
	disabled := false
	mcpStore := newMockMCPStore(&store.MCPServer{
		Name:        "context7",
		Command:     "npx",
		Transport:   "stdio",
		Enabled:     &disabled,
		CachedTools: json.RawMessage(`[{"name":"foo"}]`),
	})

	r := platformRequest(t, "GET", "/api/mcp/status", mcpStore)
	rr := httptest.NewRecorder()
	MCPStatusHandler(rr, r)

	var resp MCPStatusResponse
	json.NewDecoder(rr.Body).Decode(&resp)

	if len(resp.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(resp.Servers))
	}
	if resp.Servers[0].Status != "disabled" {
		t.Errorf("expected status 'disabled', got %q", resp.Servers[0].Status)
	}
}

func TestMCPStatusHandler_EmptyWithoutPlatformContext(t *testing.T) {
	// Without platform context, should return empty list (not error)
	r := httptest.NewRequest("GET", "/api/mcp/status", nil)
	rr := httptest.NewRecorder()
	MCPStatusHandler(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Servers []cache.ServerStatus `json:"servers"`
	}
	json.NewDecoder(rr.Body).Decode(&resp)
	if len(resp.Servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(resp.Servers))
	}
}

// =============================================================================
// mcpManagerForRequest scope tests
// =============================================================================

func TestMCPManagerForRequest_PlatformScope(t *testing.T) {
	platformStore := newMockMCPStore(&store.MCPServer{
		Name:    "context7",
		Command: "npx",
		Args:    []string{"-y", "@upstash/context7-mcp"},
	})
	orgStore := newMockMCPStore()  // empty
	teamStore := newMockMCPStore() // empty

	r := platformRequestWithScopes(t, "GET", "/api/mcp/context7/tools?scope=platform", orgStore, teamStore, platformStore)

	mgr, cfg, err := mcpManagerForRequest(r, "context7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Command != "npx" {
		t.Fatalf("expected config command npx, got %q", cfg.Command)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	mgr.Cleanup()
}

func TestMCPManagerForRequest_TeamScope(t *testing.T) {
	teamStore := newMockMCPStore(&store.MCPServer{
		Name:    "team-server",
		Command: "node",
	})
	orgStore := newMockMCPStore()
	platformStore := newMockMCPStore()

	r := platformRequestWithScopes(t, "GET", "/api/mcp/team-server/tools?scope=team", orgStore, teamStore, platformStore)

	mgr, cfg, err := mcpManagerForRequest(r, "team-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Command != "node" {
		t.Fatalf("expected config command node, got %q", cfg.Command)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	mgr.Cleanup()
}

func TestMCPManagerForRequest_OrgDefault(t *testing.T) {
	orgStore := newMockMCPStore(&store.MCPServer{
		Name:    "org-server",
		Command: "python",
	})
	teamStore := newMockMCPStore()
	platformStore := newMockMCPStore()

	// No scope param → defaults to org
	r := platformRequestWithScopes(t, "GET", "/api/mcp/org-server/tools", orgStore, teamStore, platformStore)

	mgr, cfg, err := mcpManagerForRequest(r, "org-server")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Command != "python" {
		t.Fatalf("expected config command python, got %q", cfg.Command)
	}
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	mgr.Cleanup()
}

func TestMCPManagerForRequest_NotFoundInScope(t *testing.T) {
	// Server exists in platform store, but request uses org scope (no scope param)
	platformStore := newMockMCPStore(&store.MCPServer{
		Name:    "context7",
		Command: "npx",
	})
	orgStore := newMockMCPStore() // empty — server NOT here

	r := platformRequestWithScopes(t, "GET", "/api/mcp/context7/tools", orgStore, newMockMCPStore(), platformStore)

	_, _, err := mcpManagerForRequest(r, "context7")
	if err == nil {
		t.Fatal("expected error when server not in the scoped store")
	}
	want := "server 'context7' not found in config"
	if err.Error() != want {
		t.Errorf("got error %q, want %q", err.Error(), want)
	}
}

func TestMCPManagerForRequest_NoStore(t *testing.T) {
	// No platform context at all
	r := httptest.NewRequest("GET", "/api/mcp/context7/tools", nil)
	r = mux.SetURLVars(r, map[string]string{"serverName": "context7"})

	_, _, err := mcpManagerForRequest(r, "context7")
	if err == nil {
		t.Fatal("expected error when no store available")
	}
	want := "MCP server store not available"
	if err.Error() != want {
		t.Errorf("got error %q, want %q", err.Error(), want)
	}
}

// =============================================================================
// UninstallStandardServerHandler tests
// =============================================================================

func TestUninstallStandardServer_RemovesFromBothStores(t *testing.T) {
	// Isolate both global secret backends from any process-level stores.
	secrets, legacy, _ := standardServerSecretFixture(t)

	// Setup mock MCP store with tavily
	mcpStore := newMockMCPStore(&store.MCPServer{
		Name:    "tavily",
		Command: "npx",
	})

	// Create request with platform context and ?scope=platform
	r := httptest.NewRequest("DELETE", "/api/standard-servers/tavily?scope=platform", nil)
	svc := &store.Services{
		Mode:               store.ModePlatform,
		PlatformMCPServers: mcpStore,
	}
	ctx := store.WithServices(r.Context(), svc)
	r = r.WithContext(WithPlatformUser(ctx, &PlatformUser{ID: "admin", PlatformRole: "superadmin"}))
	r = mux.SetURLVars(r, map[string]string{"id": "tavily"})

	rr := httptest.NewRecorder()
	UninstallStandardServerHandler(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify: secret removed from platform secrets
	if _, exists := secrets.secrets["web_servers.tavily.api_key"]; exists {
		t.Error("expected secret to be removed from platform secrets")
	}
	if len(secrets.removed) == 0 || secrets.removed[0] != "web_servers.tavily.api_key" {
		t.Errorf("expected RemoveSecret called with 'web_servers.tavily.api_key', got %v", secrets.removed)
	}

	if legacy.GetSecret("web_servers.tavily.api_key") != "" {
		t.Error("expected secret to be removed from legacy file store")
	}

	// Verify: server removed from MCP store
	if len(mcpStore.deleted) == 0 || mcpStore.deleted[0] != "tavily" {
		t.Errorf("expected Delete called with 'tavily', got %v", mcpStore.deleted)
	}
}

func TestUninstallStandardServer_NoStore_Returns503(t *testing.T) {
	// No platform context → effectiveMCPStore returns nil
	r := httptest.NewRequest("DELETE", "/api/standard-servers/tavily", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "tavily"})

	rr := httptest.NewRecorder()
	UninstallStandardServerHandler(rr, r)

	if rr.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d: %s", rr.Code, rr.Body.String())
	}
}

type standardServerEffectStore struct {
	*mockMCPServerStore
	saves, deletes, cacheUpdates int
	saveErr, deleteErr           error
}

func (s *standardServerEffectStore) Save(ctx context.Context, server *store.MCPServer) error {
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	return s.mockMCPServerStore.Save(ctx, server)
}

func (s *standardServerEffectStore) Delete(ctx context.Context, name string) error {
	s.deletes++
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return s.mockMCPServerStore.Delete(ctx, name)
}

func (s *standardServerEffectStore) UpdateCachedTools(ctx context.Context, name string, tools json.RawMessage) error {
	s.cacheUpdates++
	return s.mockMCPServerStore.UpdateCachedTools(ctx, name, tools)
}

type standardServerEffectSettings struct {
	value store.TeamSettings
	saves int
}

func (s *standardServerEffectSettings) Get(context.Context) (*store.TeamSettings, error) {
	value := s.value
	return &value, nil
}

func (s *standardServerEffectSettings) Save(_ context.Context, value *store.TeamSettings) error {
	s.saves++
	s.value = *value
	return nil
}

// Isolate both global secret backends. Comparing encrypted file bytes also catches
// writes that leave the in-memory secret value unchanged.
func standardServerSecretFixture(t *testing.T) (*mockPlatformSecrets, *credentials.Store, func()) {
	t.Helper()
	return standardServerSecretStateFixture(t, true, true)
}

func standardServerSecretStateFixture(t *testing.T, databasePresent, legacyPresent bool) (*mockPlatformSecrets, *credentials.Store, func()) {
	t.Helper()
	const key = "web_servers.tavily.api_key"
	secrets := newMockPlatformSecrets()
	secrets.secrets["unrelated.secret"] = "unrelated-db-sentinel"
	if databasePresent {
		secrets.secrets[key] = "global-db-sentinel"
	}
	beforeSecrets := make(map[string]string, len(secrets.secrets))
	for k, v := range secrets.secrets {
		beforeSecrets[k] = v
	}
	prev := platformSecretsInstance
	platformSecretsInstance = secrets
	t.Cleanup(func() { platformSecretsInstance = prev })
	dir := t.TempDir()
	legacy, err := credentials.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := legacy.SetSecret("unrelated.secret", "unrelated-file-sentinel"); err != nil {
		t.Fatal(err)
	}
	if legacyPresent {
		if err := legacy.SetSecret(key, "global-file-sentinel"); err != nil {
			t.Fatal(err)
		}
	}
	legacyValueBefore := legacy.GetSecret(key)
	previousLegacy := getAPICredentialStore()
	SetAPICredentialStore(legacy)
	t.Cleanup(func() { SetAPICredentialStore(previousLegacy) })
	path := filepath.Join(dir, "credentials.enc")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	assertUnchanged := func() {
		t.Helper()
		if !reflect.DeepEqual(secrets.secrets, beforeSecrets) || len(secrets.removed) != 0 || len(secrets.written) != 0 {
			t.Error("global DB secret changed")
		}
		if legacy.GetSecret(key) != legacyValueBefore || legacy.GetSecret("unrelated.secret") != "unrelated-file-sentinel" {
			t.Error("legacy global secret changed")
		}
		after, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(before, after) {
			t.Error("legacy credential file was rewritten")
		}
	}
	return secrets, legacy, assertUnchanged
}

func standardServerLocalInstallFixture(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)
	enabled := true
	if err := config.SaveAppConfig(&config.AppConfig{Sandbox: config.SandboxConfig{Enabled: &enabled}}); err != nil {
		t.Fatal(err)
	}
	// Exercise the real install handler without starting a sandbox or goroutine.
	if _, loaded := discoveryInFlight.LoadOrStore("tavily", struct{}{}); loaded {
		t.Fatal("unexpected discovery already in flight")
	}
	t.Cleanup(func() { discoveryInFlight.Delete("tavily") })
}

func standardServerMutationRequest(method, scope string, svc *store.Services, user *PlatformUser, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, "/api/standard-servers/tavily?scope="+scope, body)
	r = mux.SetURLVars(r, map[string]string{"id": "tavily"})
	return r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), user))
}

type standardServerEffectPlatformSettings struct {
	value           store.PlatformSettings
	saves           int
	missing         bool
	getErr, saveErr error
}

func (s *standardServerEffectPlatformSettings) Get(context.Context) (*store.PlatformSettings, error) {
	if s.getErr != nil || s.missing {
		return nil, s.getErr
	}
	value := s.value
	return &value, nil
}

func (s *standardServerEffectPlatformSettings) Save(_ context.Context, value *store.PlatformSettings) error {
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.value = *value
	return nil
}

func TestStandardServerPlatformUninstallSettingsUnavailableDoesNotFallBack(t *testing.T) {
	for _, state := range []string{"missing_store", "missing_settings", "read_error", "save_error"} {
		t.Run(state, func(t *testing.T) {
			standardServerSecretFixture(t)
			srv := config.GetStandardServer("tavily")
			team := &standardServerEffectSettings{value: store.TeamSettings{
				WebSearchTool: srv.WebSearchTool, WebExtractTool: srv.WebExtractTool, DefaultModel: "team-model",
			}}
			platform := &standardServerEffectPlatformSettings{value: store.PlatformSettings{
				WebSearchTool: srv.WebSearchTool, WebExtractTool: srv.WebExtractTool, DefaultModel: "platform-model",
			}}
			teamBefore, platformBefore := team.value, platform.value
			svc := &store.Services{
				Mode: store.ModePlatform, Settings: team, PlatformSettings: platform,
				PlatformMCPServers: newMockMCPStore(&store.MCPServer{Name: srv.ID}),
			}
			switch state {
			case "missing_store":
				svc.PlatformSettings = nil
			case "missing_settings":
				platform.missing = true
			case "read_error":
				platform.getErr = errors.New("settings unavailable")
			case "save_error":
				platform.saveErr = errors.New("settings unavailable")
			}
			w := httptest.NewRecorder()
			UninstallStandardServerHandler(w, standardServerMutationRequest(http.MethodDelete, "platform", svc, &PlatformUser{ID: "admin", PlatformRole: "superadmin"}, nil))
			if w.Code != http.StatusOK {
				t.Fatalf("best-effort cleanup status=%d body=%s", w.Code, w.Body.String())
			}
			if team.saves != 0 || !reflect.DeepEqual(team.value, teamBefore) {
				t.Error("unavailable platform settings caused team settings mutation")
			}
			if !reflect.DeepEqual(platform.value, platformBefore) {
				t.Error("failed cleanup changed persisted platform settings")
			}
			wantSaves := 0
			if state == "save_error" {
				wantSaves = 1
			}
			if platform.saves != wantSaves {
				t.Errorf("platform saves=%d want=%d", platform.saves, wantSaves)
			}
		})
	}
}

func TestStandardServerInstallUninstallSettingsScope(t *testing.T) {
	standardServerLocalInstallFixture(t)
	srv := config.GetStandardServer("tavily")
	for _, scope := range []string{"platform", "team", "org", ""} {
		for _, state := range []string{"both", "search_only", "extract_only", "neither", "other_scope_matching"} {
			t.Run(scope+"/"+state, func(t *testing.T) {
				standardServerSecretFixture(t)
				teamSettings := &standardServerEffectSettings{value: store.TeamSettings{
					DefaultProvider: "team-provider", DefaultModel: "team-model",
					WebSearchTool: "team-search", WebExtractTool: "team-extract",
					ContextLength: 8192, DisabledTools: []string{"preserved-tool"},
				}}
				platformSettings := &standardServerEffectPlatformSettings{value: store.PlatformSettings{
					DefaultProvider: "platform-provider", DefaultModel: "platform-model",
					WebSearchTool: "platform-search", WebExtractTool: "platform-extract",
					DefaultBrandTheme:   "aster",
					PerplexityWebSearch: &store.PerplexityWebSearchSettings{Provider: "sonar", Model: "sonar-pro"},
				}}
				teamBefore, platformBefore := teamSettings.value, platformSettings.value
				team, org, platform := newMockMCPStore(), newMockMCPStore(), newMockMCPStore()
				svc := &store.Services{
					Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform,
					Settings: teamSettings, PlatformSettings: platformSettings,
				}
				user := &PlatformUser{ID: "admin", Role: "admin", PlatformRole: "superadmin"}
				w := httptest.NewRecorder()
				InstallStandardServerHandler(w, standardServerMutationRequest(http.MethodPost, scope, svc, user, strings.NewReader(`{"env":{"TAVILY_API_KEY":"scoped-key"}}`)))
				if w.Code != http.StatusOK {
					t.Fatalf("install status=%d body=%s", w.Code, w.Body.String())
				}
				selected := team
				if scope == "org" {
					selected = org
				} else if scope == "platform" {
					selected = platform
				}
				if selected.servers[srv.ID] == nil {
					t.Fatal("install did not save server in selected scope")
				}
				wantTeam, wantPlatform := teamBefore, platformBefore
				if scope == "platform" {
					wantPlatform.WebSearchTool, wantPlatform.WebExtractTool = srv.WebSearchTool, srv.WebExtractTool
				} else {
					wantTeam.WebSearchTool, wantTeam.WebExtractTool = srv.WebSearchTool, srv.WebExtractTool
				}
				if !reflect.DeepEqual(teamSettings.value, wantTeam) || !reflect.DeepEqual(platformSettings.value, wantPlatform) {
					t.Fatal("install did not update only the selected settings scope")
				}

				// Simulate independently selecting another tool before uninstall.
				search, extract := srv.WebSearchTool, srv.WebExtractTool
				if state == "extract_only" || state == "neither" {
					search = "replacement-search"
				}
				if state == "search_only" || state == "neither" {
					extract = "replacement-extract"
				}
				if scope == "platform" {
					platformSettings.value.WebSearchTool, platformSettings.value.WebExtractTool = search, extract
					if state == "other_scope_matching" {
						teamSettings.value.WebSearchTool, teamSettings.value.WebExtractTool = srv.WebSearchTool, srv.WebExtractTool
					}
				} else {
					teamSettings.value.WebSearchTool, teamSettings.value.WebExtractTool = search, extract
					if state == "other_scope_matching" {
						platformSettings.value.WebSearchTool, platformSettings.value.WebExtractTool = srv.WebSearchTool, srv.WebExtractTool
					}
				}
				wantTeam, wantPlatform = teamSettings.value, platformSettings.value
				if search == srv.WebSearchTool {
					search = ""
				}
				if extract == srv.WebExtractTool {
					extract = ""
				}
				if scope == "platform" {
					wantPlatform.WebSearchTool, wantPlatform.WebExtractTool = search, extract
				} else {
					wantTeam.WebSearchTool, wantTeam.WebExtractTool = search, extract
				}
				teamSaves, platformSaves := teamSettings.saves, platformSettings.saves
				w = httptest.NewRecorder()
				UninstallStandardServerHandler(w, standardServerMutationRequest(http.MethodDelete, scope, svc, user, nil))
				if w.Code != http.StatusOK {
					t.Fatalf("uninstall status=%d body=%s", w.Code, w.Body.String())
				}
				if selected.servers[srv.ID] != nil || len(selected.deleted) != 1 {
					t.Error("uninstall did not remove selected server")
				}
				if !reflect.DeepEqual(platformSettings.value, wantPlatform) {
					t.Errorf("platform settings after uninstall = %+v, want %+v", platformSettings.value, wantPlatform)
				}
				if !reflect.DeepEqual(teamSettings.value, wantTeam) {
					t.Errorf("team settings after uninstall = %+v, want %+v", teamSettings.value, wantTeam)
				}
				if scope == "platform" && teamSettings.saves != teamSaves {
					t.Error("platform uninstall wrote team settings")
				}
				if scope != "platform" && platformSettings.saves != platformSaves {
					t.Error("narrow uninstall wrote platform settings")
				}
			})
		}
	}
}

func TestStandardServerScopedInstallUninstallPreservesGlobalSecrets(t *testing.T) {
	standardServerLocalInstallFixture(t)
	for _, global := range []struct {
		name                           string
		databasePresent, legacyPresent bool
	}{
		{"globals_present", true, true},
		{"database_key_absent", false, true},
		{"legacy_key_absent", true, false},
		{"both_keys_absent", false, false},
	} {
		for _, scope := range []string{"", "team", "org", "platform"} {
			for _, superadmin := range []bool{false, true} {
				if scope == "platform" && !superadmin {
					continue
				}
				scopeName := scope
				if scopeName == "" {
					scopeName = "default_team"
				}
				name := global.name + "/" + scopeName + "/org-admin"
				if superadmin {
					name = global.name + "/" + scopeName + "/superadmin"
				}
				t.Run(name, func(t *testing.T) {
					secrets, legacy, unchanged := standardServerSecretStateFixture(t, global.databasePresent, global.legacyPresent)
					team, org, platform := newMockMCPStore(), newMockMCPStore(), newMockMCPStore()
					svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform}
					user := &PlatformUser{ID: "admin", Role: "admin"}
					if superadmin {
						user.PlatformRole = "superadmin"
					}
					selected := team
					if scope == "org" {
						selected = org
					}
					if scope == "platform" {
						selected = platform
					}
					w := httptest.NewRecorder()
					InstallStandardServerHandler(w, standardServerMutationRequest(http.MethodPost, scope, svc, user, strings.NewReader(`{"env":{"TAVILY_API_KEY":"scoped-replacement"}}`)))
					if w.Code != http.StatusOK {
						t.Fatalf("install status=%d body=%s", w.Code, w.Body.String())
					}
					if got := selected.servers["tavily"]; got == nil || got.Env["TAVILY_API_KEY"] != "scoped-replacement" {
						t.Fatal("env not saved in selected store")
					}
					for _, other := range []*mockMCPServerStore{team, org, platform} {
						if other != selected && len(other.servers) != 0 {
							t.Fatal("install wrote another scope")
						}
					}
					if scope != "platform" {
						unchanged()
					} else if secrets.GetSecret("web_servers.tavily.api_key") != "scoped-replacement" {
						t.Error("authorized platform bridge did not write secret")
					}
					w = httptest.NewRecorder()
					UninstallStandardServerHandler(w, standardServerMutationRequest(http.MethodDelete, scope, svc, user, nil))
					if w.Code != http.StatusOK {
						t.Fatalf("uninstall status=%d body=%s", w.Code, w.Body.String())
					}
					if len(selected.servers) != 0 || len(selected.deleted) != 1 {
						t.Error("selected server not removed")
					}
					for _, other := range []*mockMCPServerStore{team, org, platform} {
						if other != selected && len(other.deleted) != 0 {
							t.Fatal("uninstall deleted another scope")
						}
					}
					if scope != "platform" {
						unchanged()
					} else if secrets.GetSecret("web_servers.tavily.api_key") != "" || legacy.GetSecret("web_servers.tavily.api_key") != "" {
						t.Error("authorized platform uninstall did not remove both secrets")
					}
				})
			}
		}
	}
}

type standardServerUnreadBody struct{ reads int }

func (b *standardServerUnreadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("body must not be read")
}

func TestStandardServerMutationRejectsBeforeEffects(t *testing.T) {
	for _, operation := range []struct {
		name, method string
		handler      http.HandlerFunc
	}{
		{"install", http.MethodPost, InstallStandardServerHandler},
		{"uninstall", http.MethodDelete, UninstallStandardServerHandler},
	} {
		for _, tc := range []struct {
			name, scope        string
			missing            bool
			role, platformRole string
			status             int
		}{
			{"unknown", "typo", false, "admin", "superadmin", 400},
			{"effective", "effective", false, "admin", "superadmin", 400},
			{"missing-default", "", true, "admin", "superadmin", 503},
			{"missing-team", "team", true, "admin", "superadmin", 503},
			{"missing-org", "org", true, "admin", "superadmin", 503},
			{"missing-platform", "platform", true, "admin", "superadmin", 503},
			{"member-team", "team", false, "member", "", 403},
			{"member-org", "org", false, "member", "", 403},
			{"org-admin-platform", "platform", false, "admin", "", 403},
		} {
			t.Run(operation.name+"/"+tc.name, func(t *testing.T) {
				_, _, unchanged := standardServerSecretFixture(t)
				newStore := func() *standardServerEffectStore {
					return &standardServerEffectStore{mockMCPServerStore: newMockMCPStore(&store.MCPServer{Name: "tavily"})}
				}
				team, org, platform := newStore(), newStore(), newStore()
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
				w := httptest.NewRecorder()
				operation.handler(w, standardServerMutationRequest(operation.method, tc.scope, svc, &PlatformUser{ID: "user", Role: tc.role, PlatformRole: tc.platformRole}, body))
				if w.Code != tc.status {
					t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
				}
				if body.reads != 0 {
					t.Error("rejected request read body")
				}
				for _, s := range []*standardServerEffectStore{team, org, platform} {
					if s.saves != 0 || s.deletes != 0 || s.cacheUpdates != 0 || len(s.servers) != 1 {
						t.Error("rejected request changed MCP store")
					}
				}
				if settings.saves != 0 {
					t.Error("rejected request changed settings")
				}
				unchanged()
			})
		}
	}
}

func TestStandardServerInstallSaveFailurePreservesSecrets(t *testing.T) {
	standardServerLocalInstallFixture(t)
	for _, scope := range []string{"team", "org", "platform"} {
		t.Run(scope, func(t *testing.T) {
			_, _, unchanged := standardServerSecretFixture(t)
			mcpStore := &standardServerEffectStore{mockMCPServerStore: newMockMCPStore(), saveErr: errors.New("save unavailable")}
			settings := &standardServerEffectSettings{}
			svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: mcpStore, MCPServers: mcpStore, PlatformMCPServers: mcpStore, Settings: settings}
			w := httptest.NewRecorder()
			InstallStandardServerHandler(w, standardServerMutationRequest(http.MethodPost, scope, svc, &PlatformUser{ID: "admin", Role: "admin", PlatformRole: "superadmin"}, strings.NewReader(`{"env":{"TAVILY_API_KEY":"replacement"}}`)))
			if w.Code != http.StatusInternalServerError {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if mcpStore.saves != 1 || len(mcpStore.servers) != 0 || mcpStore.cacheUpdates != 0 {
				t.Error("unexpected save effects")
			}
			if settings.saves != 0 {
				t.Error("failed save changed web tool settings")
			}
			unchanged()
		})
	}
}

func TestStandardServerUninstallDeleteFailurePreservesSecrets(t *testing.T) {
	for _, scope := range []string{"team", "org", "platform"} {
		t.Run(scope, func(t *testing.T) {
			_, _, unchanged := standardServerSecretFixture(t)
			mcpStore := &standardServerEffectStore{mockMCPServerStore: newMockMCPStore(&store.MCPServer{Name: "tavily"}), deleteErr: errors.New("delete unavailable")}
			settings := &standardServerEffectSettings{value: store.TeamSettings{WebSearchTool: config.GetStandardServer("tavily").WebSearchTool}}
			svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: mcpStore, MCPServers: mcpStore, PlatformMCPServers: mcpStore, Settings: settings}
			w := httptest.NewRecorder()
			UninstallStandardServerHandler(w, standardServerMutationRequest(http.MethodDelete, scope, svc, &PlatformUser{ID: "admin", Role: "admin", PlatformRole: "superadmin"}, nil))
			if w.Code != http.StatusInternalServerError {
				t.Errorf("status=%d want=500 body=%s", w.Code, w.Body.String())
			}
			if mcpStore.deletes != 1 || len(mcpStore.servers) != 1 {
				t.Error("unexpected delete effects")
			}
			if settings.saves != 0 {
				t.Error("failed delete changed web tool settings")
			}
			unchanged()
		})
	}
}

func TestUninstallStandardServer_UnknownServer_Returns404(t *testing.T) {
	r := httptest.NewRequest("DELETE", "/api/standard-servers/nonexistent", nil)
	r = mux.SetURLVars(r, map[string]string{"id": "nonexistent"})

	rr := httptest.NewRecorder()
	UninstallStandardServerHandler(rr, r)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rr.Code)
	}
}
