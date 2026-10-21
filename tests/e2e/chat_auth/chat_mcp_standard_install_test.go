//go:build e2e

// Standard MCP server install in platform mode — encryption envelope contract
// AND platform-tier cascade contract.
//
// Two related scenarios live in this file:
//
//   - CHAT-069 pins the JSONB UTF-8 regression: when ASTONISH_MASTER_KEY is set,
//     pgMCPServerStore.Save must wrap AES-GCM ciphertext in a base64 envelope
//     ({"_encrypted":"..."}) before writing to the JSONB env column. Without
//     the envelope, raw ciphertext bytes such as 0x8e violate JSONB's UTF-8
//     contract and Postgres rejects the INSERT with SQLSTATE 22021.
//
//   - CHAT-070 pins the platform-tier cascade contract: a server installed at
//     scope=platform must be visible to chat in EVERY org/team without a
//     per-org/team install. Older code only looked at org+team MCP stores at
//     chat-build time, silently hiding platform-tier servers from the agent's
//     tool list and system prompt. Pins the fix that adds Platform to
//     store.MCPServerStores and makes loadMCPConfig walk all three tiers.
//
// The master key is injected by e2eboot.BootstrapPlatformCore() so that both
// isolated and shared-inspector modes exercise the same encryption path.

package chat_auth

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SAP/astonish/pkg/api"
	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/credentials"
	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
	"github.com/SAP/astonish/tests/e2eboot"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
)

// TestE2E_SecurityUrgentLegacyMCPLocalAuthorization exercises the actual legacy
// routes over loopback HTTP, backed only by temporary SQLite. Tokens are issued
// by the test (not obtained by login), then validated by normal JWT middleware;
// team roles are resolved from persisted membership. No Bootstrap, cluster,
// provider, MCP process, or successful discovery is involved.
// COVERS: CHAT-112
func localMCPHTTPFixture(t *testing.T, override func(*http.Request) *http.Request, configure func(*httptest.Server), afterSeed func(*e2eboot.SeedResult)) (*e2eboot.Harness, *e2eboot.SeedResult, *httptest.Server, func(string) *httptest.Server) {
	t.Helper()
	// First-run Studio tests install process-global chat initialization hooks.
	// This route-only fixture must not inherit their background PreWarm when a
	// successful MCP toggle calls Reset: chat initialization creates the legacy
	// credential file independently of the scoped HTTP mutation under test.
	// Disable automatic initialization before Reset (which waits for any active
	// initialization and cleans up its components), before changing config paths.
	api.SetPreWarmContextFunc(nil)
	api.GetChatManager().Reset()
	api.SetStudioChatInitFunc(nil)
	// Never inherit first-run credential stores pointing at another fixture.
	api.SetAPICredentialStore(nil)

	ctx := context.Background()
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("SUDO_USER", "")
	dataDir := t.TempDir()
	svc, backend, err := entstore.NewPlatformServices(ctx, entstore.Config{
		DSN: "file:" + filepath.Join(dataDir, "platform.db"), DataDir: dataDir,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { backend.Close() })
	// Registered before servers/seed cleanup: stop HTTP first, then reset chat
	// before closing SQLite or restoring the environment. PreWarm remains off,
	// so teardown cannot launch a replacement background initializer.
	t.Cleanup(api.GetChatManager().Reset)
	const jwtSecret = "e2e-test-jwt-secret-that-is-at-least-32-chars-long!!"
	authCfg := config.PlatformAuthConfig{Mode: config.AuthModeBuiltin, JWTSecret: jwtSecret, LoopbackBypass: "never"}
	auth := api.NewPlatformAuth(authCfg, backend, config.StorageConfig{Backend: "sqlite", Auth: authCfg})
	var pendingAudit sync.WaitGroup
	var servers []*httptest.Server
	// Register after Seed's tenant cleanup, including Fatal/Goexit during setup.
	defer func() {
		t.Cleanup(func() {
			for _, server := range servers {
				server.Close()
			}
			pendingAudit.Wait()
			localMCPWaitDiscoveryExit(t)
		})
	}()
	serve := func(services *store.Services, unavailableScope string) *httptest.Server {
		router := mux.NewRouter()
		api.RegisterPlatformAuthRoutes(router, auth)
		tenantMW := func(next http.Handler) http.Handler {
			return entstore.TenantMiddleware(backend)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// Fault only the destination after normal JWT and tenant resolution.
				// All remaining stores are the real temporary SQLite services.
				if unavailableScope != "" {
					requestSvc := *store.FromRequest(r)
					if unavailableScope == "team" {
						requestSvc.TeamMCPServers = nil
					} else if unavailableScope == "org" {
						requestSvc.MCPServers = nil
					} else {
						requestSvc.PlatformMCPServers = nil
					}
					r = r.WithContext(store.WithServices(r.Context(), &requestSvc))
				}
				if override != nil {
					r = override(r)
				}
				requestSvc := *store.FromRequest(r)
				if requestSvc.Audit != nil && requestSvc.Mode == store.ModePlatform {
					// Add before AuditMiddleware can launch the goroutine, never
					// from inside Log: server.Close must precede the final Wait.
					pendingAudit.Add(1)
					requestSvc.Audit = localMemoryTrackedAudit{AuditStore: requestSvc.Audit, pending: &pendingAudit}
					r = r.WithContext(store.WithServices(r.Context(), &requestSvc))
				}
				next.ServeHTTP(w, r)
			}))
		}
		api.RegisterRoutes(router, services, backend, tenantMW)
		server := httptest.NewUnstartedServer(api.PlatformAuthMiddleware(auth, router))
		if configure != nil {
			configure(server)
		}
		server.Start()
		servers = append(servers, server)
		return server
	}
	server := serve(svc, "")
	h := &e2eboot.Harness{BaseURL: server.URL, Store: backend, DataDir: dataDir}
	seed := e2eboot.Seed(t, h)
	if afterSeed != nil {
		afterSeed(seed)
	}
	return h, seed, server, func(scope string) *httptest.Server { return serve(svc, scope) }
}

// COVERS: CHAT-112
func TestE2E_SecurityUrgentLegacyMCPLocalAuthorization(t *testing.T) {
	h, seed, server, serve := localMCPHTTPFixture(t, nil, nil, nil)
	backend := h.Store
	ctx := context.Background()
	const jwtSecret = "e2e-test-jwt-secret-that-is-at-least-32-chars-long!!"
	platformAdmin, teamAdmin := e2eboot.SeedProviderAuthority(t, h, seed)
	alice := seed.Users[e2eboot.UserAliceEmail]
	org, err := backend.ForOrg(alice.OrgSlug)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := backend.ForOrg(e2eboot.OrgGlobexSlug)
	if err != nil {
		t.Fatal(err)
	}
	// This principal has platform authority only: persisted org/team membership
	// roles are both member, never org owner/admin or team admin.
	platformOnly := platformAdmin.Token()
	stores := map[string]store.MCPServerStore{
		"platform": backend.PlatformMCPServers(), "org": org.OrgMCPServers(),
		"team":         org.ForTeam(alice.TeamSlug).MCPServers(),
		"sibling_team": org.ForTeam(e2eboot.TeamAcmeBlue).MCPServers(),
		"foreign_org":  foreign.OrgMCPServers(),
		"foreign_team": foreign.ForTeam(e2eboot.TeamGlobexEng).MCPServers(),
	}
	const serverName = "legacy-mcp-authority-canary"
	const missingName = "legacy-mcp-missing-no-discovery"
	for scope, target := range stores {
		enabled := true
		if err := target.Save(ctx, &store.MCPServer{
			Name: serverName, Command: "must-not-execute", Args: []string{scope},
			Env: map[string]string{"SYNTHETIC_CANARY": scope}, Transport: "stdio", Enabled: &enabled,
			CachedTools: json.RawMessage(`[{"name":"cached_canary"}]`),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A platform-only row proves omitted scope does not fall back to a merged
	// read store, even for an otherwise authorized team administrator.
	if err := stores["platform"].Save(ctx, &store.MCPServer{Name: "legacy-platform-row", Command: "must-not-execute", Transport: "stdio"}); err != nil {
		t.Fatal(err)
	}
	if err := backend.Secrets().SetSecret("legacy-mcp-canary", "synthetic-not-a-real-secret"); err != nil {
		t.Fatal(err)
	}
	encode := func(value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	snapshot := func() map[string][]byte {
		t.Helper()
		result := make(map[string][]byte)
		for scope, target := range stores {
			rows, err := target.List(ctx)
			if err != nil || len(rows) == 0 {
				t.Fatalf("non-vacuous %s snapshot: %v", scope, err)
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
			// Include caches and timestamps: no discovery is allowed in this test.
			result[scope] = encode(rows)
		}
		rows, err := backend.PlatformClient().PlatformSecret.Query().All(ctx)
		if err != nil || len(rows) == 0 {
			t.Fatalf("non-vacuous secret snapshot: %v", err)
		}
		secrets := make(map[string][]byte)
		for _, row := range rows {
			secrets[row.ID] = row.Value
		}
		result["secrets"] = encode(secrets)
		configDir, err := config.GetConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"config.yaml", "mcp_config.json", "tools_cache.json", "credentials.enc"} {
			data, err := os.ReadFile(filepath.Join(configDir, name))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			result["file/"+name] = encode(struct {
				Present bool
				Data    []byte
			}{err == nil, data})
		}
		return result
	}
	request := func(t *testing.T, target *httptest.Server, method, path, token, body string, want int) []byte {
		t.Helper()
		req, err := http.NewRequest(method, target.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		client := target.Client()
		client.Timeout = 5 * time.Second
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if err != nil || resp.StatusCode != want {
			t.Fatalf("%s %s: status=%d want=%d body=%s error=%v", method, path, resp.StatusCode, want, data, err)
		}
		return data
	}
	unchanged := func(t *testing.T, before map[string][]byte) {
		t.Helper()
		for scope, after := range snapshot() {
			if !bytes.Equal(before[scope], after) {
				t.Errorf("request changed %s persistence", scope)
			}
		}
	}
	member := seed.Client(e2eboot.UserBobEmail).Token()
	owner := seed.Client(e2eboot.UserAliceEmail).Token()
	for _, operation := range []struct{ name, method, path string }{
		{"toggle", http.MethodPatch, "/api/mcp/servers/" + serverName},
		{"refresh", http.MethodPost, "/api/mcp/" + missingName + "/refresh"},
	} {
		for _, tc := range []struct {
			name, token, query string
			want               int
		}{
			{"unauthenticated", "", "", http.StatusUnauthorized},
			{"invalid_jwt", "invalid-token", "", http.StatusUnauthorized},
			{"member_default", member, "", http.StatusForbidden},
			{"member_team", member, "?scope=team", http.StatusForbidden},
			{"member_org", member, "?scope=org", http.StatusForbidden},
			{"member_platform", member, "?scope=platform", http.StatusForbidden},
			{"team_admin_org", teamAdmin.Token(), "?scope=org", http.StatusForbidden},
			{"team_admin_platform", teamAdmin.Token(), "?scope=platform", http.StatusForbidden},
			{"team_admin_sibling", teamAdmin.Token(), "?scope=team&team=" + e2eboot.TeamAcmeBlue, http.StatusForbidden},
			{"team_admin_foreign", teamAdmin.Token(), "?scope=team&team=" + e2eboot.TeamGlobexEng, http.StatusForbidden},
			{"org_owner_platform", owner, "?scope=platform", http.StatusForbidden},
			{"platform_admin_team", platformAdmin.Token(), "?scope=team", http.StatusForbidden},
			{"platform_admin_org", platformAdmin.Token(), "?scope=org", http.StatusForbidden},
			{"platform_only_default", platformOnly, "", http.StatusForbidden},
			{"platform_only_org", platformOnly, "?scope=org", http.StatusForbidden},
			{"effective_scope", owner, "?scope=effective", http.StatusBadRequest},
			{"unknown_scope", platformOnly, "?scope=unknown", http.StatusBadRequest},
		} {
			t.Run(operation.name+"/"+tc.name, func(t *testing.T) {
				before := snapshot()
				request(t, server, operation.method, operation.path+tc.query, tc.token, `{"enabled":false}`, tc.want)
				unchanged(t, before)
			})
		}
	}
	// Missing rows are (nil, nil) in SQLite. Both authorized routes must
	// return 404, never dereference the row or start discovery.
	for _, tc := range []struct{ name, token, query string }{
		{"team_default", teamAdmin.Token(), ""}, {"team_explicit", teamAdmin.Token(), "?scope=team"},
		{"org", owner, "?scope=org"}, {"platform_only", platformOnly, "?scope=platform"},
	} {
		t.Run("authorized_missing_toggle/"+tc.name, func(t *testing.T) {
			before := snapshot()
			request(t, server, http.MethodPatch, "/api/mcp/servers/"+missingName+tc.query, tc.token, `{"enabled":false}`, http.StatusNotFound)
			request(t, server, http.MethodPatch, "/api/mcp-platform/servers/"+missingName+tc.query, tc.token, `{"enabled":false}`, http.StatusNotFound)
			unchanged(t, before)
		})
		t.Run("authorized_refresh/"+tc.name, func(t *testing.T) {
			before := snapshot()
			request(t, server, http.MethodPost, "/api/mcp/"+missingName+"/refresh"+tc.query, tc.token, "", http.StatusNotFound)
			request(t, server, http.MethodPost, "/api/mcp-platform/servers/"+missingName+"/refresh"+tc.query, tc.token, "", http.StatusNotFound)
			unchanged(t, before)
		})
	}
	t.Run("default_team_does_not_mutate_platform_row", func(t *testing.T) {
		before := snapshot()
		request(t, server, http.MethodPatch, "/api/mcp/servers/legacy-platform-row", teamAdmin.Token(), `{"enabled":false}`, http.StatusNotFound)
		unchanged(t, before)
	})
	for _, missing := range []struct{ scope, token string }{
		{"team", teamAdmin.Token()}, {"platform", platformOnly},
	} {
		unavailableServer := serve(missing.scope)
		for _, operation := range []struct{ method, path string }{
			{http.MethodPatch, "/api/mcp/servers/" + serverName},
			{http.MethodPost, "/api/mcp/" + missingName + "/refresh"},
		} {
			t.Run("missing_"+missing.scope+"_store/"+operation.method, func(t *testing.T) {
				before := snapshot()
				request(t, unavailableServer, operation.method, operation.path+"?scope="+missing.scope, missing.token, `{"enabled":false}`, http.StatusServiceUnavailable)
				unchanged(t, before)
			})
		}
	}
	for _, tc := range []struct {
		name, token, query, scope string
		enabled                   bool
	}{
		{"explicit_team", teamAdmin.Token(), "?scope=team", "team", false},
		{"omitted_means_team", teamAdmin.Token(), "", "team", true},
		{"org_owner", owner, "?scope=org", "org", false},
		{"platform_only", platformOnly, "?scope=platform", "platform", false},
	} {
		t.Run("persisted_toggle/"+tc.name, func(t *testing.T) {
			before := snapshot()
			data := request(t, server, http.MethodPatch, "/api/mcp/servers/"+serverName+tc.query, tc.token, string(encode(map[string]bool{"enabled": tc.enabled})), http.StatusOK)
			var response api.UpdateMCPServerResponse
			if err := json.Unmarshal(data, &response); err != nil || !response.Success || response.Server.Name != serverName || response.Server.Enabled != tc.enabled {
				t.Fatalf("toggle response did not report persisted state: %s (%v)", data, err)
			}
			persisted, err := stores[tc.scope].Get(ctx, serverName)
			if err != nil || persisted == nil || persisted.IsEnabled() != tc.enabled {
				t.Fatalf("toggle not persisted in %s: %v", tc.scope, err)
			}
			var expected []store.MCPServer
			if err := json.Unmarshal(before[tc.scope], &expected); err != nil {
				t.Fatal(err)
			}
			for i := range expected {
				if expected[i].Name == serverName {
					expected[i].Enabled = &tc.enabled
					expected[i].UpdatedAt = persisted.UpdatedAt
				}
			}
			before[tc.scope] = encode(expected)
			unchanged(t, before)
		})
	}

	// The stdio preflight requires sandbox enabled, but an intentionally invalid
	// backend stops asynchronous discovery before any external connection. The
	// denial record is evidence only; join the actual goroutine before name reuse.
	enabled := true
	if err := config.SaveAppConfig(&config.AppConfig{Sandbox: config.SandboxConfig{Enabled: &enabled, Backend: "e2e-no-discovery"}}); err != nil {
		t.Fatal(err)
	}
	priorLogger := slog.Default()
	priorLogWriter, priorLogFlags := log.Writer(), log.Flags()
	discoveryDenied := make(chan struct{}, 8)
	// Do not wrap slog's default handler: SetDefault redirects log output into
	// this handler, which would recursively lock log's mutex on the first log.
	slog.SetDefault(slog.New(&localMCPDiscoveryLog{Handler: slog.NewTextHandler(io.Discard, nil), denied: discoveryDenied}))
	t.Cleanup(func() {
		localMCPWaitDiscoveryExit(t)
		slog.SetDefault(priorLogger)
		log.SetOutput(priorLogWriter)
		log.SetFlags(priorLogFlags)
	})
	api.SetPlatformSecrets(backend.Secrets())
	t.Cleanup(func() { api.SetPlatformSecrets(nil) })
	standard := config.GetStandardServer("tavily")
	if standard == nil || standard.WebSearchTool == "" || standard.WebExtractTool == "" {
		t.Fatal("standard server fixture must provide both web tool references")
	}
	teamSettings := org.ForTeam(alice.TeamSlug).Settings()
	platformSettings := backend.PlatformSettings()
	for _, state := range []string{"both", "search_only", "extract_only", "neither", "other_scope_matching"} {
		t.Run("platform_settings_install_uninstall/"+state, func(t *testing.T) {
			team := &store.TeamSettings{DefaultProvider: "team-provider", DefaultModel: "team-model",
				WebSearchTool: "team-search", WebExtractTool: "team-extract", ContextLength: 8192,
				DisabledTools: []string{"preserved-tool"}}
			if state == "other_scope_matching" {
				team.WebSearchTool, team.WebExtractTool = standard.WebSearchTool, standard.WebExtractTool
			}
			platform := &store.PlatformSettings{DefaultProvider: "platform-provider", DefaultModel: "platform-model",
				WebSearchTool: "platform-search", WebExtractTool: "platform-extract", DefaultBrandTheme: "aster",
				PerplexityWebSearch: &store.PerplexityWebSearchSettings{Provider: "sonar", Model: "sonar-pro"}}
			if err := teamSettings.Save(ctx, team); err != nil {
				t.Fatal(err)
			}
			if err := platformSettings.Save(ctx, platform); err != nil {
				t.Fatal(err)
			}
			// Read back canonical persisted values, not just our input structs.
			team, err = teamSettings.Get(ctx)
			if err != nil || team == nil {
				t.Fatalf("team settings fixture: %v", err)
			}
			platform, err = platformSettings.Get(ctx)
			if err != nil || platform == nil {
				t.Fatalf("platform settings fixture: %v", err)
			}
			teamBefore := encode(team)
			assertSettings := func() {
				t.Helper()
				gotTeam, err := teamSettings.Get(ctx)
				if err != nil || !bytes.Equal(encode(gotTeam), teamBefore) {
					t.Fatalf("platform operation changed team settings: %v", err)
				}
				gotPlatform, err := platformSettings.Get(ctx)
				if err != nil || !bytes.Equal(encode(gotPlatform), encode(platform)) {
					t.Fatalf("platform settings do not match selected references/preserved fields: got=%s want=%s error=%v", encode(gotPlatform), encode(platform), err)
				}
			}
			before := snapshot()
			// No env value: settings routing does not need a key, and unrelated
			// secret ciphertext must remain byte-for-byte identical.
			body := request(t, server, http.MethodPost, "/api/standard-servers/tavily/install?scope=platform", platformOnly, `{"env":{}}`, http.StatusOK)
			var result struct{ Status, ServerName string }
			if err := json.Unmarshal(body, &result); err != nil || result.Status != "installed" || result.ServerName != standard.ID {
				t.Fatalf("install response: %s (%v)", body, err)
			}
			select {
			case <-discoveryDenied:
			case <-time.After(5 * time.Second):
				t.Fatal("local discovery did not emit denial before backend access")
			}
			localMCPWaitDiscoveryExit(t)
			persisted, err := stores["platform"].Get(ctx, standard.ID)
			if err != nil || persisted == nil || persisted.Command != standard.Command || len(persisted.CachedTools) != 0 {
				t.Fatalf("standard server was not persisted without discovery: %v", err)
			}
			platform.WebSearchTool, platform.WebExtractTool = standard.WebSearchTool, standard.WebExtractTool
			assertSettings()
			installed := snapshot()
			installed["platform"] = before["platform"]
			for scope, value := range before {
				if !bytes.Equal(value, installed[scope]) {
					t.Errorf("install changed unrelated %s persistence", scope)
				}
			}
			// Simulate choosing another provider before removal. Uninstall must
			// clear only references still matching this standard server.
			if state == "extract_only" || state == "neither" {
				platform.WebSearchTool = "replacement-search"
			}
			if state == "search_only" || state == "neither" {
				platform.WebExtractTool = "replacement-extract"
			}
			if err := platformSettings.Save(ctx, platform); err != nil {
				t.Fatal(err)
			}
			body = request(t, server, http.MethodDelete, "/api/standard-servers/tavily?scope=platform", platformOnly, "", http.StatusOK)
			if err := json.Unmarshal(body, &result); err != nil || result.Status != "uninstalled" || result.ServerName != standard.ID {
				t.Fatalf("uninstall response: %s (%v)", body, err)
			}
			if platform.WebSearchTool == standard.WebSearchTool {
				platform.WebSearchTool = ""
			}
			if platform.WebExtractTool == standard.WebExtractTool {
				platform.WebExtractTool = ""
			}
			assertSettings()
			persisted, err = stores["platform"].Get(ctx, standard.ID)
			if err != nil || persisted != nil {
				t.Fatalf("uninstall left the platform row: %v", err)
			}
			unchanged(t, before)
		})
	}

	// Additional identities are local to this fixture: never change Bob, Alice,
	// or SeedProviderAuthority's meanings to obtain org-admin coverage.
	orgAdminID := uuid.NewString()
	const orgAdminEmail = "local-mcp-org-admin@example.invalid"
	if err := backend.Users().Create(ctx, &store.User{ID: orgAdminID, Email: orgAdminEmail, DisplayName: "MCP org admin", Status: "active", CreatedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	organization, err := backend.Organizations().GetBySlug(ctx, alice.OrgSlug)
	if err != nil || organization == nil {
		t.Fatalf("org admin organization: %v", err)
	}
	team, err := org.Teams().GetTeamBySlug(ctx, alice.TeamSlug)
	if err != nil || team == nil {
		t.Fatalf("org admin team: %v", err)
	}
	if err := backend.Organizations().AddMember(ctx, orgAdminID, organization.ID, "admin"); err != nil {
		t.Fatal(err)
	}
	if err := org.Teams().AddMember(ctx, &store.TeamMembership{UserID: orgAdminID, TeamID: team.ID, Role: "member", JoinedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	persistedUser, err := backend.Users().GetByID(ctx, orgAdminID)
	if err != nil || persistedUser == nil || persistedUser.PlatformRole != "" {
		t.Fatalf("org admin must have no platform role: %v", err)
	}
	orgRole, err := backend.Organizations().GetMemberRole(ctx, orgAdminID, organization.ID)
	if err != nil || orgRole != "admin" {
		t.Fatalf("persisted org admin role: %q (%v)", orgRole, err)
	}
	teamRole, err := org.Teams().GetMemberRole(ctx, orgAdminID, team.ID)
	if err != nil || teamRole != "member" {
		t.Fatalf("org admin must only be team member: %q (%v)", teamRole, err)
	}
	orgAdmin, err := api.NewJWTIssuer(jwtSecret, 15*time.Minute, 24*time.Hour).IssueAccessToken(orgAdminID, orgAdminEmail, "MCP org admin", alice.OrgSlug, alice.TeamSlug, orgRole, "")
	if err != nil {
		t.Fatal(err)
	}
	platformUser, err := backend.Users().GetByEmail(ctx, "urgent-platform-admin@example.invalid")
	if err != nil || platformUser == nil {
		t.Fatalf("platform principal: %v", err)
	}

	// Denial records carry the selected executable configuration, proving
	// refresh read the requested scope even though discovery cannot run. Use an
	// independent sink, never the previous default handler (log mutex recursion).
	records := make(chan slog.Record, 128)
	slog.SetDefault(slog.New(&localMCPDiscoveryLog{Handler: slog.NewTextHandler(io.Discard, nil), records: records}))
	waitDiscovery := func(t *testing.T, name, command string) {
		t.Helper()
		select {
		case record := <-records:
			attrs := map[string]string{}
			record.Attrs(func(attr slog.Attr) bool { attrs[attr.Key] = attr.Value.String(); return true })
			if record.Message != "async MCP discovery: no tools discovered" || attrs["server"] != name || attrs["command"] != command {
				t.Fatalf("unexpected discovery denial: %s %v", record.Message, attrs)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("discovery did not emit denial at the invalid local backend")
		}
		localMCPWaitDiscoveryExit(t)
	}
	// Add settings to the exact snapshot for this batch, including sibling and
	// foreign scopes. The original 56 cases above retain their existing fixture.
	batchSnapshot := func() map[string][]byte {
		result := snapshot()
		for name, settings := range map[string]store.SettingsStore{
			"team": teamSettings, "sibling_team": org.ForTeam(e2eboot.TeamAcmeBlue).Settings(),
			"foreign_team": foreign.ForTeam(e2eboot.TeamGlobexEng).Settings(),
		} {
			value, err := settings.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			result["settings/"+name] = encode(value)
		}
		for name, slug := range map[string]string{"org": alice.OrgSlug, "foreign_org": e2eboot.OrgGlobexSlug} {
			value, err := backend.OrgSettings(slug).Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			result["settings/"+name] = encode(value)
		}
		value, err := platformSettings.Get(ctx)
		if err != nil {
			t.Fatal(err)
		}
		result["settings/platform"] = encode(value)
		return result
	}
	assertBatch := func(t *testing.T, want map[string][]byte) {
		t.Helper()
		for scope, got := range batchSnapshot() {
			if !bytes.Equal(got, want[scope]) {
				t.Errorf("unexpected %s persistence: got=%s want=%s", scope, got, want[scope])
			}
		}
	}
	// Only DB-generated identity/time fields may be adopted from a new row;
	// updates retain exact ID/CreatedAt and must advance UpdatedAt within the
	// request interval. All other fields, caches and unrelated rows are exact.
	expectSaved := func(t *testing.T, before map[string][]byte, scope string, want store.MCPServer, started time.Time, replace bool) {
		t.Helper()
		got, err := stores[scope].Get(ctx, want.Name)
		if err != nil || got == nil {
			t.Fatalf("saved %s row: %v", scope, err)
		}
		var rows []store.MCPServer
		if err := json.Unmarshal(before[scope], &rows); err != nil {
			t.Fatal(err)
		}
		index := -1
		for i := range rows {
			if rows[i].Name == want.Name {
				index = i
				break
			}
		}
		if got.ID == "" || got.UpdatedAt.Before(started) || got.UpdatedAt.After(time.Now()) {
			t.Fatalf("invalid generated identity/update time: %+v", got)
		}
		if index < 0 {
			if got.CreatedAt.Before(started) || got.CreatedAt.After(time.Now()) {
				t.Fatal("creation time outside request")
			}
			want.ID, want.CreatedAt = got.ID, got.CreatedAt
		} else {
			want.ID, want.CreatedAt = rows[index].ID, rows[index].CreatedAt
			if !got.UpdatedAt.After(rows[index].UpdatedAt) {
				t.Fatal("update timestamp did not advance")
			}
		}
		want.UpdatedAt = got.UpdatedAt
		if replace {
			rows = []store.MCPServer{want}
		} else if index < 0 {
			rows = append(rows, want)
		} else {
			rows[index] = want
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
		before[scope] = encode(rows)
		assertBatch(t, before)
	}
	scopes := []struct{ name, scope, query, token, userID string }{
		{"org_admin_default", "team", "", orgAdmin, orgAdminID},
		{"org_admin_team", "team", "?scope=team", orgAdmin, orgAdminID},
		{"org_admin_org", "org", "?scope=org", orgAdmin, orgAdminID},
		{"platform_only", "platform", "?scope=platform", platformOnly, platformUser.ID},
	}
	for _, tc := range scopes {
		// A unique name per scope avoids the process-wide discovery de-dup gate;
		// the same name is deliberately present in every other scoped store.
		name := "local-mcp-" + tc.name
		for scope, target := range stores {
			if scope == tc.scope {
				continue
			}
			if err := target.Save(ctx, &store.MCPServer{Name: name, Command: "must-not-execute-" + scope, Transport: "stdio", Env: map[string]string{"TOKEN": scope}}); err != nil {
				t.Fatal(err)
			}
		}
		current := store.MCPServer{Name: name, Command: "must-not-execute-create", Args: []string{"create"}, Env: map[string]string{"TOKEN": "synthetic-create"}, Transport: "stdio", Enabled: &enabled, CreatedBy: tc.userID}
		for _, operation := range []string{"create", "update", "toggle", "inline_install", "legacy_settings", "legacy_patch", "delete"} {
			t.Run("scoped_mutation/"+tc.name+"/"+operation, func(t *testing.T) {
				before := batchSnapshot()
				method, path, body, status, discover := http.MethodPost, "/api/mcp-platform/servers", "", http.StatusCreated, true
				switch operation {
				case "create":
					body = string(encode(current))
				case "update":
					current.Command, current.Args, current.Env = "must-not-execute-update", []string{"update"}, map[string]string{"TOKEN": "synthetic-update"}
					method, path, body, status = http.MethodPut, "/api/mcp-platform/servers/"+name, string(encode(current)), http.StatusOK
				case "toggle", "legacy_patch":
					disabled := false
					current.Enabled = &disabled
					method, path, body, status, discover = http.MethodPatch, "/api/mcp-platform/servers/"+name, `{"enabled":false}`, http.StatusOK, false
					if operation == "legacy_patch" {
						path = "/api/mcp/servers/" + name
					}
				case "inline_install", "legacy_settings":
					current.Command, current.Args, current.Env, current.Enabled = "must-not-execute-"+operation, []string{operation}, map[string]string{"TOKEN": "synthetic-" + operation}, &enabled
					cfg := config.MCPServerConfig{Command: current.Command, Args: current.Args, Env: current.Env, Transport: current.Transport, Enabled: current.Enabled}
					path, body, status = "/api/mcp/install-inline", string(encode(map[string]any{"serverName": name, "config": cfg})), http.StatusOK
					if operation == "legacy_settings" {
						method, path, body = http.MethodPut, "/api/settings/mcp", string(encode(config.MCPConfig{MCPServers: map[string]config.MCPServerConfig{name: cfg}}))
					}
				case "delete":
					method, path, status, discover = http.MethodDelete, "/api/mcp-platform/servers/"+name, http.StatusOK, false
				}
				started := time.Now()
				response := request(t, server, method, path+tc.query, tc.token, body, status)
				if strings.Contains(string(response), "synthetic-") {
					t.Fatal("mutation response leaked inline secret")
				}
				if discover {
					waitDiscovery(t, name, current.Command)
				}
				if operation == "delete" {
					// Legacy settings replaced the entire selected scope with this row.
					// Keep a separate sentinel so snapshots never become vacuous.
					var rows []store.MCPServer
					if err := json.Unmarshal(before[tc.scope], &rows); err != nil {
						t.Fatal(err)
					}
					kept := rows[:0]
					for _, row := range rows {
						if row.Name != name {
							kept = append(kept, row)
						}
					}
					before[tc.scope] = encode(kept)
					assertBatch(t, before)
				} else {
					expectSaved(t, before, tc.scope, current, started, operation == "legacy_settings")
				}
			})
			if operation == "legacy_settings" {
				if err := stores[tc.scope].Save(ctx, &store.MCPServer{Name: serverName, Command: "must-not-execute", Transport: "stdio", Enabled: &enabled}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}

	for _, tc := range scopes {
		for _, route := range []struct {
			name, prefix, suffix string
			status               int
		}{
			{"modern", "/api/mcp-platform/servers/", "/refresh", http.StatusAccepted},
			{"legacy", "/api/mcp/", "/refresh", http.StatusOK},
		} {
			t.Run("existing_refresh/"+tc.name+"/"+route.name, func(t *testing.T) {
				name := "local-refresh-" + tc.name + "-" + route.name
				for scope, target := range stores {
					if err := target.Save(ctx, &store.MCPServer{Name: name, Command: "must-not-execute-" + scope, Args: []string{scope}, Transport: "stdio", Enabled: &enabled, Env: map[string]string{"TOKEN": "synthetic-" + scope}, CachedTools: json.RawMessage(`[{"name":"retained_cache"}]`)}); err != nil {
						t.Fatal(err)
					}
				}
				before := batchSnapshot()
				response := request(t, server, http.MethodPost, route.prefix+name+route.suffix+tc.query, tc.token, "", route.status)
				var result struct {
					Status, Name string
					Success      bool
				}
				if err := json.Unmarshal(response, &result); err != nil || (route.name == "modern" && (result.Status != "refresh_started" || result.Name != name)) || (route.name == "legacy" && !result.Success) {
					t.Fatalf("refresh acknowledgement: %s (%v)", response, err)
				}
				waitDiscovery(t, name, "must-not-execute-"+tc.scope)
				assertBatch(t, before)
			})
		}
	}

	// Narrow standard installs must not bridge synthetic credentials into the
	// existing global Tavily slot. Org scope intentionally uses team web-tool
	// settings, matching the current legacy contract, not org settings.
	if err := backend.Secrets().SetSecret("web_servers.tavily.api_key", "synthetic-global-retained"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range scopes[:3] {
		t.Run("org_admin_standard_install_uninstall/"+tc.name, func(t *testing.T) {
			for scope, target := range stores {
				if scope == tc.scope {
					if err := target.Delete(ctx, standard.ID); err != nil {
						t.Fatal(err)
					}
					continue
				}
				if err := target.Save(ctx, &store.MCPServer{Name: standard.ID, Command: "must-not-execute-" + scope, Transport: "stdio", Env: map[string]string{"TOKEN": scope}, CachedTools: json.RawMessage(`[{"name":"other_tavily"}]`)}); err != nil {
					t.Fatal(err)
				}
			}
			before := batchSnapshot()
			var expectedSettings store.TeamSettings
			if err := json.Unmarshal(before["settings/team"], &expectedSettings); err != nil {
				t.Fatal(err)
			}
			expectedSettings.WebSearchTool, expectedSettings.WebExtractTool = standard.WebSearchTool, standard.WebExtractTool
			started := time.Now()
			response := request(t, server, http.MethodPost, "/api/standard-servers/tavily/install"+tc.query, tc.token, `{"env":{"TAVILY_API_KEY":"synthetic-scoped-tavily"}}`, http.StatusOK)
			if strings.Contains(string(response), "synthetic-scoped-tavily") {
				t.Fatal("standard install leaked scoped secret")
			}
			var result struct{ Status, ServerName string }
			if err := json.Unmarshal(response, &result); err != nil || result.Status != "installed" || result.ServerName != standard.ID {
				t.Fatalf("standard install response: %s (%v)", response, err)
			}
			waitDiscovery(t, standard.ID, standard.Command)
			before["settings/team"] = encode(expectedSettings)
			expectSaved(t, before, tc.scope, store.MCPServer{Name: standard.ID, Command: standard.Command, Args: standard.Args, Env: map[string]string{"TAVILY_API_KEY": "synthetic-scoped-tavily"}, Transport: "stdio", Enabled: &enabled, CreatedBy: tc.userID}, started, false)
			response = request(t, server, http.MethodDelete, "/api/standard-servers/tavily"+tc.query, tc.token, "", http.StatusOK)
			if err := json.Unmarshal(response, &result); err != nil || result.Status != "uninstalled" || result.ServerName != standard.ID {
				t.Fatalf("standard uninstall response: %s (%v)", response, err)
			}
			var rows []store.MCPServer
			if err := json.Unmarshal(before[tc.scope], &rows); err != nil {
				t.Fatal(err)
			}
			kept := rows[:0]
			for _, row := range rows {
				if row.Name != standard.ID {
					kept = append(kept, row)
				}
			}
			before[tc.scope] = encode(kept)
			expectedSettings.WebSearchTool, expectedSettings.WebExtractTool = "", ""
			before["settings/team"] = encode(expectedSettings)
			assertBatch(t, before)
		})
	}

	// Handler tests own body-read/store-call tripwires. Here the finite route
	// matrix proves middleware role resolution and actual HTTP wiring, with
	// valid mutation bodies and exact DB/cache/time/secret/file/settings effects.
	missingTeamServer, missingOrgServer, missingPlatformServer := serve("team"), serve("org"), serve("platform")
	for _, operation := range []struct{ name, method, path, body string }{
		{"create", http.MethodPost, "/api/mcp-platform/servers", `{"name":"` + serverName + `","command":"must-not-execute-denied","transport":"stdio","env":{"TOKEN":"synthetic-denied"}}`},
		{"update", http.MethodPut, "/api/mcp-platform/servers/" + serverName, `{"command":"must-not-execute-denied","env":{"TOKEN":"synthetic-denied"}}`},
		{"delete", http.MethodDelete, "/api/mcp-platform/servers/" + serverName, ""},
		{"toggle", http.MethodPatch, "/api/mcp-platform/servers/" + serverName, `{"enabled":false}`},
		{"refresh", http.MethodPost, "/api/mcp-platform/servers/" + serverName + "/refresh", ""},
		{"inline_install", http.MethodPost, "/api/mcp/install-inline", `{"serverName":"` + serverName + `","config":{"command":"must-not-execute-denied","transport":"stdio","env":{"TOKEN":"synthetic-denied"}}}`},
		{"legacy_settings", http.MethodPut, "/api/settings/mcp", `{"mcpServers":{"` + serverName + `":{"command":"must-not-execute-denied","transport":"stdio","env":{"TOKEN":"synthetic-denied"}}}}`},
		{"legacy_patch", http.MethodPatch, "/api/mcp/servers/" + serverName, `{"enabled":false}`},
		{"legacy_refresh", http.MethodPost, "/api/mcp/" + serverName + "/refresh", ""},
		{"standard_install", http.MethodPost, "/api/standard-servers/tavily/install", `{"env":{"TAVILY_API_KEY":"synthetic-denied"}}`},
		{"standard_uninstall", http.MethodDelete, "/api/standard-servers/tavily", ""},
	} {
		for _, tc := range []struct {
			name, token, query string
			target             *httptest.Server
			status             int
		}{
			{"unauthenticated", "", "", server, http.StatusUnauthorized},
			{"member_default", member, "", server, http.StatusForbidden},
			{"member_team", member, "?scope=team", server, http.StatusForbidden},
			{"member_org", member, "?scope=org", server, http.StatusForbidden},
			{"member_platform", member, "?scope=platform", server, http.StatusForbidden},
			{"org_admin_platform", orgAdmin, "?scope=platform", server, http.StatusForbidden},
			{"team_admin_org", teamAdmin.Token(), "?scope=org", server, http.StatusForbidden},
			{"platform_only_default", platformOnly, "", server, http.StatusForbidden},
			{"effective_scope", orgAdmin, "?scope=effective", server, http.StatusBadRequest},
			{"unknown_scope", orgAdmin, "?scope=unknown", server, http.StatusBadRequest},
			{"missing_default_team_store", orgAdmin, "", missingTeamServer, http.StatusServiceUnavailable},
			{"missing_org_store", orgAdmin, "?scope=org", missingOrgServer, http.StatusServiceUnavailable},
			{"missing_team_store", orgAdmin, "?scope=team", missingTeamServer, http.StatusServiceUnavailable},
			{"missing_platform_store", platformOnly, "?scope=platform", missingPlatformServer, http.StatusServiceUnavailable},
		} {
			// Legacy member/scope/missing-store rejection is already covered by
			// the original cases; add only their new independent org-admin case.
			if (operation.name == "legacy_patch" || operation.name == "legacy_refresh") && tc.name != "org_admin_platform" {
				continue
			}
			t.Run("remaining_rejected/"+operation.name+"/"+tc.name, func(t *testing.T) {
				before := batchSnapshot()
				response := request(t, tc.target, operation.method, operation.path+tc.query, tc.token, operation.body, tc.status)
				if strings.Contains(string(response), "synthetic-") {
					t.Fatal("rejection exposed persisted/request secret")
				}
				assertBatch(t, before)
				select {
				case record := <-records:
					t.Fatalf("denied request started discovery: %s", record.Message)
				default:
				}
			})
		}
	}
	// MASK coverage is deliberately separate from the authorization/CRUD matrix.
	// All URLs are inert metadata on stdio configs: the invalid local sandbox
	// backend above prevents either executable launch or outbound HTTP discovery.
	const maskURL = "https://synthetic-user:synthetic-pass@service.invalid/mcp%2fpath?z=synthetic-signature%2B%2f&a=synthetic-first&a=synthetic-second&empty=#synthetic-fragment"
	const maskedURL = "https://%2A%2A%2A%2A:%2A%2A%2A%2A@service.invalid/mcp%2fpath?a=%2A%2A%2A%2A&a=%2A%2A%2A%2A&empty=&z=%2A%2A%2A%2A#****"
	maskHTTP := func(t *testing.T, method, path, token string, payload any, status int) []byte {
		t.Helper()
		var body []byte
		if payload != nil {
			body = encode(payload)
		}
		req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		client := server.Client()
		client.Timeout = 5 * time.Second
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		// Unlike request's small mutation responses, legacy GET returns the whole
		// selected store. Still bound reads and reject truncation explicitly.
		data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
		if err != nil || len(data) > 1<<20 || resp.StatusCode != status {
			t.Fatalf("masked HTTP %s %s: status=%d want=%d bytes=%d error=%v", method, path, resp.StatusCode, status, len(data), err)
		}
		if bytes.Contains(data, []byte("synthetic-")) {
			t.Fatal("masked HTTP response exposed a synthetic credential")
		}
		if status >= 400 && (len(data) == 0 || len(data) > 512) {
			t.Fatalf("unbounded/empty mask error: %d bytes", len(data))
		}
		return data
	}
	for _, target := range []struct{ scope, token, userID string }{
		{"team", orgAdmin, orgAdminID}, {"org", orgAdmin, orgAdminID}, {"platform", platformOnly, platformUser.ID},
	} {
		for _, route := range []string{"modern", "legacy", "inline"} {
			cases := []string{"nonexistent_env", "nonexistent_url", "inherited_env", "inherited_url", "wrong_target_env", "wrong_target_url", "wrong_key", "changed_masked_url", "changed_destination", "changed_command"}
			if route == "inline" {
				cases = append(cases, "existing_mask_rejected", "replace_credentials", "clear_credentials")
			} else {
				cases = append(cases, "get_mask_roundtrip", "omitted_env", "empty_env", "replace_credentials", "clear_credentials", "changed_args", "changed_transport", "omitted_env_changed_destination")
				if route == "modern" {
					cases = append(cases, "omitted_env_url", "null_env_url")
				}
			}
			for _, scenario := range cases {
				t.Run("masked_update/"+target.scope+"/"+route+"/"+scenario, func(t *testing.T) {
					name := "mask-local-" + target.scope + "-" + route + "-" + scenario
					original := store.MCPServer{Name: name, Command: "must-not-execute-mask", Args: []string{"--fixed"}, Transport: "stdio", URL: maskURL, Enabled: &enabled, CreatedBy: target.userID,
						Env: map[string]string{"ARBITRARY": "synthetic-literal", "MIXED": "synthetic-prefix-{{CREDENTIAL:fixture:token}}", "REF": "{{CREDENTIAL:fixture:token}}", "EMPTY": ""}}
					if err := stores[target.scope].Save(ctx, &original); err != nil {
						t.Fatal(err)
					}
					// Same-name decoys in every other scope prove successful mask
					// restoration uses the exact selected store, not an inherited row.
					for scope, destination := range stores {
						if scope == target.scope {
							continue
						}
						decoy := original
						decoy.Env = map[string]string{"ARBITRARY": "synthetic-decoy-" + scope}
						decoy.URL = "https://service.invalid/other?key=synthetic-decoy-" + scope
						if err := destination.Save(ctx, &decoy); err != nil {
							t.Fatal(err)
						}
					}
					query := "?scope=" + target.scope
					beforeRead := batchSnapshot()
					var view api.MCPServerListItem
					if route == "legacy" {
						data := maskHTTP(t, http.MethodGet, "/api/settings/mcp"+query, target.token, nil, http.StatusOK)
						var cfg config.MCPConfig
						if err := json.Unmarshal(data, &cfg); err != nil {
							t.Fatal(err)
						}
						v, ok := cfg.MCPServers[name]
						if !ok {
							t.Fatal("legacy GET omitted selected server")
						}
						view = api.MCPServerListItem{Name: name, Command: v.Command, Args: v.Args, Env: v.Env, Transport: v.Transport, URL: v.URL, Enabled: true}
					} else {
						data := maskHTTP(t, http.MethodGet, "/api/mcp-platform/servers/"+name+query, target.token, nil, http.StatusOK)
						if err := json.Unmarshal(data, &view); err != nil {
							t.Fatal(err)
						}
					}
					wantEnv := map[string]string{"ARBITRARY": "****", "MIXED": "****", "REF": "{{CREDENTIAL:fixture:token}}", "EMPTY": ""}
					if view.Name != name || view.URL != maskedURL || !bytes.Equal(encode(view.Env), encode(wantEnv)) {
						t.Fatalf("GET did not return the exact masked view: url=%q env=%s", view.URL, encode(view.Env))
					}
					assertBatch(t, beforeRead)
					input := map[string]any{"command": view.Command, "args": view.Args, "transport": view.Transport, "url": view.URL, "env": view.Env, "enabled": view.Enabled}
					writeName, want := name, original
					status := http.StatusBadRequest
					switch scenario {
					case "get_mask_roundtrip":
						status = http.StatusOK
					case "omitted_env":
						delete(input, "env")
						status = http.StatusOK
					case "omitted_env_url":
						delete(input, "env")
						delete(input, "url")
						status = http.StatusOK
					case "null_env_url":
						input["env"], input["url"] = nil, nil
						status = http.StatusOK
					case "empty_env":
						input["env"], want.Env = map[string]string{}, map[string]string{}
						status = http.StatusOK
					case "replace_credentials", "clear_credentials":
						want.Command = "must-not-execute-replaced"
						want.URL = "https://replacement.invalid/mcp?key=synthetic-replacement"
						want.Env = map[string]string{"ARBITRARY": "synthetic-replacement"}
						if scenario == "clear_credentials" {
							want.Env, want.URL = map[string]string{}, ""
						}
						input["command"], input["env"], input["url"] = want.Command, want.Env, want.URL
						status = http.StatusOK
					case "nonexistent_env", "nonexistent_url", "inherited_env", "inherited_url":
						if err := stores[target.scope].Delete(ctx, name); err != nil {
							t.Fatal(err)
						}
						if route == "modern" && target.scope == "team" {
							// teamMCPServerStore.Get reports missing rows as errors,
							// unlike org/platform's (nil, nil). The existing API
							// therefore fails closed at load with a bounded 500.
							status = http.StatusInternalServerError
						}
						if strings.HasPrefix(scenario, "nonexistent_") {
							writeName = name + "-absent-everywhere"
						}
						if strings.HasSuffix(scenario, "_env") {
							input["url"] = ""
						} else {
							input["env"] = map[string]string{}
						}
					case "wrong_target_env", "wrong_target_url":
						writeName = name + "-other"
						other := original
						other.Name, other.Env, other.URL = writeName, map[string]string{"OTHER": "synthetic-other-target"}, "https://other.invalid/mcp?key=synthetic-other-target"
						if err := stores[target.scope].Save(ctx, &other); err != nil {
							t.Fatal(err)
						}
						if strings.HasSuffix(scenario, "_env") {
							input["url"] = ""
						} else {
							input["env"] = map[string]string{}
						}
					case "wrong_key":
						input["env"] = map[string]string{"NONEXISTENT": "****"}
					case "changed_masked_url":
						input["env"] = map[string]string{}
						input["url"] = strings.Replace(view.URL, "service.invalid", "other.invalid", 1)
					case "changed_destination", "omitted_env_changed_destination":
						input["url"] = "https://other.invalid/mcp"
						if scenario == "omitted_env_changed_destination" {
							delete(input, "env")
						}
					case "changed_command":
						input["command"] = "must-not-execute-other"
					case "changed_args":
						input["args"] = []string{"--other"}
					case "changed_transport":
						input["transport"] = "sse"
					case "existing_mask_rejected":
						// Inline is creation-style, even for a same-name row.
					default:
						t.Fatal("unhandled mask scenario")
					}
					method, path, body := http.MethodPut, "/api/mcp-platform/servers/"+writeName, any(input)
					if route == "legacy" {
						path = "/api/settings/mcp"
						batch := map[string]any{writeName: input}
						if status != http.StatusOK {
							// A valid item and omitted existing rows make this a
							// non-vacuous validate-before-any-save/delete assertion.
							batch["valid-mask-batch-item"] = map[string]any{"command": "must-not-execute-valid", "transport": "stdio", "env": map[string]string{"ARBITRARY": "synthetic-valid-batch"}}
						}
						body = map[string]any{"mcpServers": batch}
					} else if route == "inline" {
						method, path = http.MethodPost, "/api/mcp/install-inline"
						body = map[string]any{"serverName": writeName, "config": input}
					}
					before := batchSnapshot()
					started := time.Now()
					data := maskHTTP(t, method, path+query, target.token, body, status)
					if status == http.StatusOK {
						var ack struct{ Status string }
						if err := json.Unmarshal(data, &ack); err != nil || (ack.Status != "ok" && ack.Status != "installed") {
							t.Fatal("missing successful write acknowledgement")
						}
						waitDiscovery(t, name, want.Command)
						expectSaved(t, before, target.scope, want, started, route == "legacy")
					} else {
						assertBatch(t, before)
						select {
						case record := <-records:
							t.Fatalf("invalid mask started discovery: %s", record.Message)
						default:
						}
					}
				})
				// Legacy bulk writes can replace the original sentinel; keep the
				// subsequent missing-row snapshots non-vacuous without weakening them.
				if err := stores[target.scope].Save(ctx, &store.MCPServer{Name: serverName, Command: "must-not-execute", Transport: "stdio", Enabled: &enabled}); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
}

type localMCPClosingListener struct {
	net.Listener
	closed func()
}

func (l *localMCPClosingListener) Close() error {
	err := l.Listener.Close()
	l.closed()
	return err
}

func TestE2E_MCPFixtureDrainsAuditBeforeDatabaseCleanup(t *testing.T) {
	for _, duringSetup := range []bool{false, true} {
		name := "normal"
		if duringSetup {
			name = "setup_goexit"
		}
		t.Run(name, func(t *testing.T) {
			audit := &localMemoryBlockedAudit{started: make(chan struct{}), release: make(chan struct{}), done: make(chan error, 1)}
			var release sync.Once
			defer release.Do(func() { close(audit.release) })
			t.Run("fixture", func(t *testing.T) {
				request := func(seed *e2eboot.SeedResult) {
					resp := seed.Client(e2eboot.UserAliceEmail).Get(t, "/api/mcp-platform/servers?scope=team")
					io.Copy(io.Discard, resp.Body)
					resp.Body.Close()
					if resp.StatusCode != http.StatusOK {
						t.Fatalf("status=%d", resp.StatusCode)
					}
					select {
					case <-audit.started:
					case <-time.After(5 * time.Second):
						t.Fatal("audit never started")
					}
				}
				var setup func(*e2eboot.SeedResult)
				if duringSetup {
					setup = func(seed *e2eboot.SeedResult) { request(seed); t.Skip("exercise setup Goexit cleanup") }
				}
				_, seed, _, _ := localMCPHTTPFixture(t, func(r *http.Request) *http.Request {
					svc := *store.FromRequest(r)
					if svc.Audit != nil {
						audit.AuditStore = svc.Audit
						svc.Audit = audit
					}
					return r.WithContext(store.WithServices(r.Context(), &svc))
				}, func(server *httptest.Server) {
					server.Listener = &localMCPClosingListener{Listener: server.Listener, closed: func() {
						// The listener's actual Close is the deterministic release barrier.
						release.Do(func() { close(audit.release) })
					}}
				}, setup)
				request(seed)
			})
			select {
			case err := <-audit.done:
				if err != nil {
					t.Fatalf("audit outlived tenant database: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("fixture did not drain audit")
			}
		})
	}
}

func localMCPCanarySnapshot(ctx context.Context, target store.MCPServerStore, name string) (*store.MCPServer, error) {
	// List has consistent absence semantics across team, org and platform stores.
	// Do not reinterpret Get errors: they may also be genuine storage failures.
	rows, err := target.List(ctx)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		if rows[i].Name == name {
			return &rows[i], nil
		}
	}
	return nil, nil
}

func TestE2E_MCPFixtureCanarySnapshot(t *testing.T) {
	seed, backend, _ := localMemoryHTTPFixture(t, func(r *http.Request) *http.Request { return r })
	ctx := context.Background()
	alice := seed.Users[e2eboot.UserAliceEmail]
	org, err := backend.ForOrg(alice.OrgSlug)
	if err != nil {
		t.Fatal(err)
	}
	for scope, target := range map[string]store.MCPServerStore{
		"team": org.ForTeam(alice.TeamSlug).MCPServers(), "org": org.OrgMCPServers(), "platform": backend.PlatformMCPServers(),
	} {
		t.Run(scope, func(t *testing.T) {
			row, err := localMCPCanarySnapshot(ctx, target, "tavily")
			if err != nil || row != nil {
				t.Fatalf("fresh canary slot: row=%v error=%v", row, err)
			}
			if err := target.Save(ctx, &store.MCPServer{Name: "tavily-decoy", Command: "must-not-execute"}); err != nil {
				t.Fatal(err)
			}
			row, err = localMCPCanarySnapshot(ctx, target, "tavily")
			if err != nil || row != nil {
				t.Fatalf("decoy matched absent canary: row=%v error=%v", row, err)
			}
			if err := target.Save(ctx, &store.MCPServer{Name: "tavily", Command: "must-not-execute", Env: map[string]string{"TOKEN": scope}}); err != nil {
				t.Fatal(err)
			}
			want, err := target.Get(ctx, "tavily")
			if err != nil {
				t.Fatal(err)
			}
			row, err = localMCPCanarySnapshot(ctx, target, "tavily")
			gotJSON, _ := json.Marshal(row)
			wantJSON, _ := json.Marshal(want)
			if err != nil || !bytes.Equal(gotJSON, wantJSON) {
				t.Fatalf("existing canary changed: %v", err)
			}
			if err := target.Delete(ctx, "tavily"); err != nil {
				t.Fatal(err)
			}
			row, err = localMCPCanarySnapshot(ctx, target, "tavily")
			if err != nil || row != nil {
				t.Fatalf("deleted canary slot: row=%v error=%v", row, err)
			}
			canceled, cancel := context.WithCancel(ctx)
			cancel()
			if _, err := localMCPCanarySnapshot(canceled, target, "tavily"); err == nil {
				t.Fatal("snapshot swallowed storage error")
			}
		})
	}
}

func TestE2E_MCPFixtureDiscoveryLogEvidence(t *testing.T) {
	for _, tc := range []struct {
		message string
		denied  bool
	}{
		{"async MCP discovery: no tools discovered", true},
		{"MCP discovery already in progress, skipping duplicate", false},
		{"async MCP discovery: tools cached", false},
	} {
		t.Run(tc.message, func(t *testing.T) {
			denied := make(chan struct{}, 1)
			records := make(chan slog.Record, 1)
			h := &localMCPDiscoveryLog{Handler: slog.NewTextHandler(io.Discard, nil), denied: denied, records: records}
			record := slog.NewRecord(time.Now(), slog.LevelWarn, tc.message, 0)
			record.AddAttrs(slog.String("server", "tavily"))
			if err := h.Handle(context.Background(), record); err != nil {
				t.Fatal(err)
			}
			if (len(denied) == 1) != tc.denied || (len(records) == 1) != tc.denied {
				t.Fatalf("log classified as denied=%v, want %v: %s", len(denied) == 1, tc.denied, tc.message)
			}
		})
	}
}

// localMCPDiscoveryActive reports the actual async discovery goroutine, including
// its deferred cleanup. Only use in these serial local fixtures, never as a
// product API. Fail closed if the runtime snapshot is too large to inspect.
func localMCPDiscoveryActive() bool {
	for size := 64 << 10; size <= 16<<20; size *= 2 {
		stacks := make([]byte, size)
		n := runtime.Stack(stacks, true)
		if n == len(stacks) {
			continue
		}
		return bytes.Contains(stacks[:n], []byte("\ngithub.com/SAP/astonish/pkg/api.asyncDiscoverAndCacheTools.func1("))
	}
	return true
}

func localMCPWaitDiscoveryExit(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for localMCPDiscoveryActive() {
		if time.Now().After(deadline) {
			t.Fatal("local MCP discovery goroutine did not exit")
		}
		runtime.Gosched()
	}
}

func TestE2E_MCPFixtureDiscoveryLifecycle(t *testing.T) {
	h, seed, server, _ := localMCPHTTPFixture(t, nil, nil, nil)
	platformAdmin, _ := e2eboot.SeedProviderAuthority(t, h, seed)
	enabled := true
	if err := config.SaveAppConfig(&config.AppConfig{Sandbox: config.SandboxConfig{Enabled: &enabled, Backend: "e2e-no-discovery"}}); err != nil {
		t.Fatal(err)
	}
	priorLogger := slog.Default()
	priorWriter, priorFlags := log.Writer(), log.Flags()
	denied := make(chan slog.Record, 1)
	release := make(chan struct{})
	var once sync.Once
	slog.SetDefault(slog.New(&localMCPDiscoveryLog{
		Handler: slog.NewTextHandler(io.Discard, nil), records: denied,
		afterDenial: func() { <-release },
	}))
	t.Cleanup(func() {
		once.Do(func() { close(release) })
		localMCPWaitDiscoveryExit(t)
		slog.SetDefault(priorLogger)
		log.SetOutput(priorWriter)
		log.SetFlags(priorFlags)
	})
	name := "local-lifecycle-" + uuid.NewString()
	request := func(method, path, body string, status int) {
		t.Helper()
		req, err := http.NewRequest(method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+platformAdmin.Token())
		req.Header.Set("Content-Type", "application/json")
		client := server.Client()
		client.Timeout = 5 * time.Second
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if _, err := io.Copy(io.Discard, resp.Body); err != nil || resp.StatusCode != status {
			t.Fatalf("lifecycle HTTP status=%d want=%d error=%v", resp.StatusCode, status, err)
		}
	}
	request(http.MethodPost, "/api/mcp-platform/servers?scope=platform", `{"name":"`+name+`","command":"must-not-execute","transport":"stdio"}`, http.StatusCreated)
	select {
	case <-denied:
	case <-time.After(5 * time.Second):
		t.Fatal("local discovery did not emit denial")
	}
	if !localMCPDiscoveryActive() {
		t.Fatal("denial was mistaken for completion while its log handler is still blocked")
	}
	once.Do(func() { close(release) })
	localMCPWaitDiscoveryExit(t)
	// Reusing the exact name must start a fresh attempt, not a duplicate skip.
	request(http.MethodPost, "/api/mcp-platform/servers/"+name+"/refresh?scope=platform", "", http.StatusAccepted)
	select {
	case <-denied:
	case <-time.After(5 * time.Second):
		t.Fatal("same-name refresh did not emit its own denial")
	}
	localMCPWaitDiscoveryExit(t)
}

// localMCPDiscoveryLog observes denial evidence, NOT goroutine completion.
// tools_cache.go logs before deferred discoveryInFlight.Delete; no supported
// external barrier exposes that deletion. Duplicate skips and successful cache
// writes are not denial evidence. This fixture never claims real tool discovery.
type localMCPDiscoveryLog struct {
	slog.Handler
	denied      chan<- struct{}
	records     chan<- slog.Record
	afterDenial func()
}

func (h *localMCPDiscoveryLog) Handle(ctx context.Context, record slog.Record) error {
	err := h.Handler.Handle(ctx, record)
	if record.Message == "async MCP discovery: no tools discovered" {
		if h.records != nil {
			h.records <- record.Clone()
		}
		record.Attrs(func(attr slog.Attr) bool {
			if h.denied != nil && attr.Key == "server" && attr.Value.String() == "tavily" {
				h.denied <- struct{}{}
			}
			return true
		})
		if h.afterDenial != nil {
			h.afterDenial()
		}
	}
	return err
}

// TestE2E_StandardMCPInstall_PlatformEncryptionEnvelope installs a standard
// MCP server with a non-empty env in platform scope and asserts the env
// round-trips through the JSONB-encrypted-at-rest envelope.
//
// COVERS: CHAT-069
func TestE2E_StandardMCPInstall_PlatformEncryptionEnvelope(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	// Use a fake but realistic API key value containing a byte that would
	// trigger SQLSTATE 22021 in the buggy path. AES-GCM ciphertext for any
	// payload will produce non-UTF-8 bytes; the value itself only needs to be
	// non-trivial enough that envJSON > 2 bytes (the gate in Save()).
	const apiKey = "tvly-test-CHAT069-do-not-use-in-prod-AAAAAAAAA"

	installBody := map[string]any{
		"env": map[string]string{
			"TAVILY_API_KEY": apiKey,
		},
	}

	// Install with platform scope: this is the only path where pgMCPServerStore
	// is constructed with a non-nil secrets store and so encrypts env at rest.
	resp := h.Post(t, "/api/standard-servers/tavily/install?scope=platform", installBody)
	body := e2eboot.ReadBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		// Surface the exact error so a regression to the JSONB-rejects-ciphertext
		// path is diagnosable from the failure log alone.
		if strings.Contains(body, "invalid byte sequence for encoding") || strings.Contains(body, "22021") {
			t.Fatalf("CHAT-069 regression: encryption envelope missing — JSONB rejected ciphertext: %s", body)
		}
		t.Fatalf("install returned %d: %s", resp.StatusCode, body)
	}

	// Round-trip read: the server must come back with the env decrypted to its
	// original plaintext value. This validates both Save's envelope-write and
	// decryptEnv's envelope-read.
	listResp := h.Get(t, "/api/mcp-platform/servers?scope=platform")
	defer listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list servers returned %d: %s", listResp.StatusCode, e2eboot.ReadBody(t, listResp))
	}

	// Response shape varies; decode loosely and look for the server by name.
	var raw any
	if err := json.NewDecoder(listResp.Body).Decode(&raw); err != nil {
		t.Fatalf("decode servers list: %v", err)
	}

	got := findEnvForServer(raw, "tavily", "TAVILY_API_KEY")
	if got == "" {
		// Provide the raw response for diagnosability — schema may have shifted.
		dump, _ := json.Marshal(raw)
		t.Fatalf("could not locate tavily TAVILY_API_KEY in platform servers list: %s", string(dump))
	}
	if got != apiKey {
		t.Fatalf("env round-trip mismatch: want %q, got %q", apiKey, got)
	}
}

// TestE2E_StandardMCPInstall_PlatformCascadeVisibleToOrgChat installs Tavily
// at scope=platform, then verifies the platform-tier cascade contract: a
// server with cached_tools at the platform tier MUST be visible to org/team-
// scoped GET /api/tools without any per-org/team install.
//
// This test isolates the cascade logic from sandbox-based discovery. After
// install, it writes synthetic cached_tools directly to the platform MCP store
// (simulating what asyncDiscoverAndCacheTools would produce), then asserts
// visibility through the same HTTP path the chat agent uses.
//
// Real end-to-end discovery (sandbox container creation → MCP server startup →
// tool listing → DB caching) is covered by TestE2E_Apps_MCPDataSource which
// exercises the full path with a real Tavily API key and ensureBaseConfigured.
//
// COVERS: CHAT-070
func TestE2E_StandardMCPInstall_PlatformCascadeVisibleToOrgChat(t *testing.T) {
	h := e2eboot.Bootstrap(t)

	const apiKey = "tvly-test-CHAT070-do-not-use-in-prod-BBBBBBBBB"

	installBody := map[string]any{
		"env": map[string]string{
			"TAVILY_API_KEY": apiKey,
		},
	}

	resp := h.Post(t, "/api/standard-servers/tavily/install?scope=platform", installBody)
	body := e2eboot.ReadBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("install returned %d: %s", resp.StatusCode, body)
	}

	// Simulate what async discovery would produce: write cached_tools directly
	// to the platform MCP store. This isolates the cascade contract test from
	// sandbox availability, Node.js presence, and network access to npm.
	syntheticTools := json.RawMessage(`[{"name":"tavily_search","description":"Search the web using Tavily"}]`)
	platformMCP := h.PlatformBackend().PlatformMCPServers()
	if err := platformMCP.UpdateCachedTools(context.Background(), "tavily", syntheticTools); err != nil {
		t.Fatalf("failed to write synthetic cached_tools: %v", err)
	}

	// The cascade contract: platform-tier cached_tools MUST be visible to an
	// org/team-scoped /api/tools request (the same path chat uses).
	if !hasToolInList(t, h, "tavily_search") {
		dump := dumpToolList(t, h)
		t.Fatalf("CHAT-070 regression: platform-tier tavily_search not visible to org-scoped /api/tools call. "+
			"This means platform MCP servers are not cascading into the chat tool list. Tools seen: %s", dump)
	}
}

// TestE2E_SecurityUrgentMCPInstallIsolation uses the registered HTTP routes and
// seeded JWT identities, not handler injection. Synthetic keys exercise storage
// isolation; successful tool discovery (and a real Tavily key) is not required.
// Do not run in shared-inspector mode: this test owns a global canary temporarily.
// COVERS: CHAT-110
func TestE2E_SecurityUrgentMCPInstallIsolation(t *testing.T) {
	h := e2eboot.Bootstrap(t)
	if h.SharedMode {
		t.Fatal("MCP install isolation requires an isolated harness")
	}
	seed := e2eboot.Seed(t, h)
	// This opt-in fixture persists org-member roles for both principals; the
	// team admin is not an org owner and the platform admin is a team member.
	platformAdmin, teamAdmin := e2eboot.SeedProviderAuthority(t, h, seed)
	const teamAdminEmail = "urgent-team-admin@example.invalid"
	const platformAdminEmail = "urgent-platform-admin@example.invalid"
	alice, bob := seed.Users[e2eboot.UserAliceEmail], seed.Users[e2eboot.UserBobEmail]
	carol, dave := seed.Users[e2eboot.UserCarolEmail], seed.Users[e2eboot.UserDaveEmail]
	if alice.OrgRole != "owner" || alice.TeamRole != "admin" || bob.TeamRole != "member" ||
		alice.OrgSlug != bob.OrgSlug || alice.TeamSlug != bob.TeamSlug ||
		carol.OrgSlug != alice.OrgSlug || carol.TeamSlug == alice.TeamSlug || dave.OrgSlug == alice.OrgSlug {
		t.Fatal("invalid MCP scope authority fixtures")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	backend := h.PlatformBackend()
	acme, err := backend.ForOrg(alice.OrgSlug)
	if err != nil {
		t.Fatal("cannot open home org store")
	}
	foreign, err := backend.ForOrg(dave.OrgSlug)
	if err != nil {
		t.Fatal("cannot open foreign org store")
	}
	stores := map[string]store.MCPServerStore{
		"platform":     backend.PlatformMCPServers(),
		"org":          acme.OrgMCPServers(),
		"team":         acme.ForTeam(alice.TeamSlug).MCPServers(),
		"sibling_team": acme.ForTeam(carol.TeamSlug).MCPServers(),
		"foreign_org":  foreign.OrgMCPServers(),
		"foreign_team": foreign.ForTeam(dave.TeamSlug).MCPServers(),
	}
	encode := func(t *testing.T, value any) []byte {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal("cannot encode persistence snapshot")
		}
		return data
	}
	// Compare encrypted bytes as well as row existence, so even rewriting the
	// same plaintext or deleting an unrelated global secret is detected.
	secretSnapshot := func(t *testing.T) []byte {
		t.Helper()
		rows, err := h.Store.PlatformClient().PlatformSecret.Query().All(ctx)
		if err != nil {
			t.Fatal("cannot snapshot global secrets")
		}
		values := make(map[string][]byte, len(rows))
		for _, row := range rows {
			values[row.ID] = row.Value
		}
		return encode(t, values)
	}
	const globalKey = "web_servers.tavily.api_key"
	const globalCanary = "urgent-mcp-global-canary-not-a-real-key"
	originalSecret, err := h.Store.PlatformClient().PlatformSecret.Query().All(ctx)
	if err != nil {
		t.Fatal("cannot preserve global canary slot")
	}
	var originalValue []byte
	var originalPresent bool
	for _, row := range originalSecret {
		if row.ID == globalKey {
			originalValue, originalPresent = append([]byte(nil), row.Value...), true
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cleanupCancel()
		if originalPresent {
			// A broken uninstall may have removed the row; recreate then restore
			// the exact ciphertext rather than re-encrypting operator material.
			if err := h.Store.Secrets().SetSecret(globalKey, globalCanary); err != nil {
				t.Error("cannot recreate original global secret slot")
				return
			}
			if err := h.Store.PlatformClient().PlatformSecret.UpdateOneID(globalKey).SetValue(originalValue).Exec(cleanupCtx); err != nil {
				t.Error("cannot restore original global secret")
			}
		} else if err := h.Store.PlatformClient().PlatformSecret.DeleteOneID(globalKey).Exec(cleanupCtx); err != nil {
			// A security regression may already have deleted the canary.
			exists, queryErr := h.Store.PlatformClient().PlatformSecret.Query().All(cleanupCtx)
			if queryErr != nil {
				t.Error("cannot verify global canary cleanup")
			}
			for _, row := range exists {
				if row.ID == globalKey {
					t.Error("cannot remove global canary")
				}
			}
		}
	})
	if err := h.Store.Secrets().SetSecret(globalKey, globalCanary); err != nil || backend.SecretGetter()(globalKey) != globalCanary {
		t.Fatal("cannot establish readable synthetic global secret")
	}
	configDir, err := config.GetConfigDir()
	if err != nil {
		t.Fatal("cannot locate legacy credential store")
	}
	legacySnapshot := func(t *testing.T) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(configDir, "credentials.enc"))
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			t.Fatal("cannot snapshot legacy global credentials")
		}
		return encode(t, data) // distinguishes an existing empty file from absence
	}
	globalBefore, legacyBefore := secretSnapshot(t), legacySnapshot(t)
	// Platform authority permits only Tavily's global slot to change. Compare
	// every other encrypted DB row, and the legacy store semantically because
	// removing its Tavily fallback legitimately re-encrypts the whole file.
	withoutTavilySecret := func(t *testing.T, data []byte) []byte {
		t.Helper()
		var values map[string][]byte
		if err := json.Unmarshal(data, &values); err != nil {
			t.Fatal("cannot decode global secret snapshot")
		}
		delete(values, globalKey)
		return encode(t, values)
	}
	legacyContents := func(t *testing.T, omitTavily bool) []byte {
		t.Helper()
		if legacySnapshot(t) == nil {
			return nil
		}
		cs, err := credentials.Open(configDir)
		if err != nil {
			t.Fatal("cannot read legacy credentials for platform scope comparison")
		}
		secrets := make(map[string]string)
		for _, key := range cs.ListSecrets() {
			if !omitTavily || key != globalKey {
				secrets[key] = cs.GetSecret(key)
			}
		}
		creds := make(map[string]*credentials.Credential)
		for name := range cs.List() {
			creds[name] = cs.Get(name)
		}
		return encode(t, struct {
			Secrets     map[string]string
			Credentials map[string]*credentials.Credential
			Migrated    bool
		}{secrets, creds, cs.HasMigrated()})
	}
	snapshot := func(t *testing.T) map[string][]byte {
		t.Helper()
		result := make(map[string][]byte)
		for name, target := range stores {
			rows, err := target.List(ctx)
			if err != nil {
				t.Fatalf("cannot snapshot %s MCP store", name)
			}
			// Discovery is asynchronous. Cache/timestamps are not configuration
			// authority; compare every persisted configuration field instead.
			for i := range rows {
				rows[i].CachedTools = nil
				rows[i].UpdatedAt = time.Time{}
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
			result[name] = encode(t, rows)
		}
		return result
	}
	for _, tc := range []struct {
		name, email, scope, target, forgedTeam string
		absentGlobalKeys                       bool
		want                                   int
	}{
		{name: "member_default_team_denied", email: e2eboot.UserBobEmail, want: http.StatusForbidden},
		{name: "member_team_denied", email: e2eboot.UserBobEmail, scope: "team", want: http.StatusForbidden},
		{name: "member_org_denied", email: e2eboot.UserBobEmail, scope: "org", want: http.StatusForbidden},
		{name: "member_platform_denied", email: e2eboot.UserBobEmail, scope: "platform", want: http.StatusForbidden},
		{name: "owner_platform_denied", email: e2eboot.UserAliceEmail, scope: "platform", want: http.StatusForbidden},
		{name: "effective_scope_denied", email: e2eboot.UserAliceEmail, scope: "effective", want: http.StatusBadRequest},
		{name: "unknown_scope_denied", email: e2eboot.UserAliceEmail, scope: "not-a-scope", want: http.StatusBadRequest},
		{name: "foreign_team_denied", email: e2eboot.UserBobEmail, scope: "team", forgedTeam: dave.TeamSlug, want: http.StatusForbidden},
		{name: "default_team_install_uninstall_preserves_globals", email: e2eboot.UserAliceEmail, target: "team", want: http.StatusOK},
		{name: "team_install_uninstall_preserves_globals", email: e2eboot.UserAliceEmail, scope: "team", target: "team", want: http.StatusOK},
		{name: "org_install_uninstall_preserves_globals", email: e2eboot.UserAliceEmail, scope: "org", target: "org", want: http.StatusOK},
		{name: "team_admin_default_team_install_uninstall_preserves_globals", email: teamAdminEmail, target: "team", want: http.StatusOK},
		{name: "team_admin_team_install_uninstall_preserves_globals", email: teamAdminEmail, scope: "team", target: "team", want: http.StatusOK},
		{name: "team_admin_org_denied", email: teamAdminEmail, scope: "org", want: http.StatusForbidden},
		{name: "team_admin_platform_denied", email: teamAdminEmail, scope: "platform", want: http.StatusForbidden},
		// Org owners intentionally bypass team membership for management;
		// an independent team admin must not gain access to a sibling team.
		{name: "team_admin_sibling_team_denied", email: teamAdminEmail, scope: "team", forgedTeam: carol.TeamSlug, want: http.StatusForbidden},
		{name: "absent_globals_default_team_install_uninstall", email: e2eboot.UserAliceEmail, target: "team", absentGlobalKeys: true, want: http.StatusOK},
		{name: "absent_globals_team_install_uninstall", email: teamAdminEmail, scope: "team", target: "team", absentGlobalKeys: true, want: http.StatusOK},
		{name: "absent_globals_org_install_uninstall", email: e2eboot.UserAliceEmail, scope: "org", target: "org", absentGlobalKeys: true, want: http.StatusOK},
		{name: "absent_globals_member_denied", email: e2eboot.UserBobEmail, scope: "team", absentGlobalKeys: true, want: http.StatusForbidden},
		{name: "platform_admin_platform_install_uninstall_scoped_effects", email: platformAdminEmail, scope: "platform", target: "platform", want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The isolated harness owns this path. Restore both initial states,
			// including a file unexpectedly created by a failing request.
			legacyPath := filepath.Join(configDir, "credentials.enc")
			originalLegacy, err := os.ReadFile(legacyPath)
			legacyPresent := err == nil
			if err != nil && !os.IsNotExist(err) {
				t.Fatal("cannot preserve isolated legacy credentials")
			}
			var legacyMode os.FileMode
			if legacyPresent {
				info, err := os.Stat(legacyPath)
				if err != nil {
					t.Fatal("cannot preserve legacy credential mode")
				}
				legacyMode = info.Mode()
			}
			t.Cleanup(func() {
				if !legacyPresent {
					if err := os.Remove(legacyPath); err != nil && !os.IsNotExist(err) {
						t.Error("cannot remove test-created legacy credentials")
					}
					return
				}
				if err := os.WriteFile(legacyPath, originalLegacy, legacyMode); err != nil {
					t.Error("cannot restore legacy credential ciphertext")
				}
				if err := os.Chmod(legacyPath, legacyMode); err != nil {
					t.Error("cannot restore legacy credential mode")
				}
			})
			globalBefore, legacyBefore := globalBefore, legacyBefore
			wantGlobal := globalCanary
			if tc.absentGlobalKeys {
				var originalRows map[string][]byte
				if err := json.Unmarshal(globalBefore, &originalRows); err != nil {
					t.Fatal("cannot preserve global ciphertext")
				}
				t.Cleanup(func() {
					cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					if err := h.Store.Secrets().SetSecret(globalKey, globalCanary); err != nil {
						t.Error("cannot recreate global canary slot")
						return
					}
					if err := h.Store.PlatformClient().PlatformSecret.UpdateOneID(globalKey).SetValue(originalRows[globalKey]).Exec(cleanupCtx); err != nil {
						t.Error("cannot restore global canary ciphertext")
					}
				})
				if err := h.Store.PlatformClient().PlatformSecret.DeleteOneID(globalKey).Exec(ctx); err != nil {
					t.Fatal("cannot prepare absent global DB key")
				}
				if legacyBefore != nil {
					cs, err := credentials.Open(configDir)
					if err != nil {
						t.Fatal("cannot open isolated legacy credentials")
					}
					if err := cs.RemoveSecret(globalKey); err != nil {
						t.Fatal("cannot prepare absent legacy key")
					}
				}
				globalBefore, legacyBefore = secretSnapshot(t), legacySnapshot(t)
				wantGlobal = ""
				if backend.SecretGetter()(globalKey) != "" {
					t.Fatal("global key remains visible in absent-key fixture")
				}
			}
			// Put a distinct, disabled canary in every scope. DELETE denials
			// must preserve an existing row, not merely succeed against absence.
			for name, target := range stores {
				original, err := localMCPCanarySnapshot(ctx, target, "tavily")
				if err != nil {
					t.Fatalf("cannot preserve %s MCP canary slot: %v", name, err)
				}
				t.Cleanup(func() {
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cleanupCancel()
					var err error
					if original == nil {
						err = target.Delete(cleanupCtx, "tavily")
					} else {
						err = target.Save(cleanupCtx, original)
					}
					if err != nil {
						t.Errorf("cannot restore %s MCP canary slot", name)
					}
				})
				enabled := false
				if err := target.Save(ctx, &store.MCPServer{Name: "tavily", Transport: "sse", URL: "https://example.invalid/urgent-mcp", Enabled: &enabled, Env: map[string]string{"TAVILY_API_KEY": "urgent-mcp-existing-" + name}}); err != nil {
					t.Fatalf("cannot seed %s MCP canary", name)
				}
			}
			// Standard installs may update the caller's web-tool selection.
			settings := acme.ForTeam(alice.TeamSlug).Settings()
			originalSettings, err := settings.Get(ctx)
			if err != nil || originalSettings == nil {
				t.Fatal("cannot preserve team settings")
			}
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cleanupCancel()
				if err := settings.Save(cleanupCtx, originalSettings); err != nil {
					t.Error("cannot restore team web-tool settings")
				}
			})
			if tc.target != "" {
				// Positive cases exercise creation, not an upsert retaining the
				// fixture's original creator and disabled flag.
				if err := stores[tc.target].Delete(ctx, "tavily"); err != nil {
					t.Fatal("cannot prepare empty scoped install slot")
				}
			}
			before := snapshot(t)
			const scopedCanary = "urgent-mcp-scoped-canary-not-a-real-key"
			var client *e2eboot.Client
			switch tc.email {
			case teamAdminEmail:
				client = teamAdmin
			case platformAdminEmail:
				client = platformAdmin
			default:
				client = seed.Client(tc.email)
			}
			actor, err := backend.Users().GetByEmail(ctx, tc.email)
			if err != nil || actor == nil {
				t.Fatal("cannot resolve authenticated MCP fixture user")
			}
			platformSettings := backend.PlatformSettings()
			originalPlatformSettings, err := platformSettings.Get(ctx)
			if err != nil || originalPlatformSettings == nil {
				t.Fatal("cannot preserve platform settings")
			}
			platformSettingsBefore := encode(t, originalPlatformSettings)
			t.Cleanup(func() {
				cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cleanupCancel()
				if err := platformSettings.Save(cleanupCtx, originalPlatformSettings); err != nil {
					t.Error("cannot restore platform settings")
				}
			})
			unrelatedSettings := map[string]store.SettingsStore{
				"sibling_team": acme.ForTeam(carol.TeamSlug).Settings(),
				"foreign_team": foreign.ForTeam(dave.TeamSlug).Settings(),
			}
			unrelatedSettingsBefore := make(map[string][]byte, len(unrelatedSettings))
			for name, target := range unrelatedSettings {
				original, err := target.Get(ctx)
				if err != nil || original == nil {
					t.Fatalf("cannot preserve %s settings", name)
				}
				unrelatedSettingsBefore[name] = encode(t, original)
				t.Cleanup(func() {
					cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cleanupCancel()
					if err := target.Save(cleanupCtx, original); err != nil {
						t.Errorf("cannot restore %s settings", name)
					}
				})
			}
			var legacyOtherBefore []byte
			if tc.target == "platform" {
				legacyOtherBefore = legacyContents(t, true)
			}
			for _, method := range []string{http.MethodPost, http.MethodDelete} {
				path := "/api/standard-servers/tavily"
				body := ""
				if method == http.MethodPost {
					path += "/install"
					body = string(encode(t, map[string]any{"env": map[string]string{"TAVILY_API_KEY": scopedCanary}}))
				}
				if tc.scope != "" {
					path += "?scope=" + tc.scope
				}
				req, err := http.NewRequestWithContext(ctx, method, h.BaseURL+path, strings.NewReader(body))
				if err != nil {
					t.Fatal("cannot construct MCP request")
				}
				req.Header.Set("Authorization", "Bearer "+client.Token())
				req.Header.Set("Content-Type", "application/json")
				team := client.TeamSlug()
				if tc.forgedTeam != "" {
					team = tc.forgedTeam
				}
				req.Header.Set("X-Astonish-Team", team)
				resp, err := (&http.Client{Timeout: 45 * time.Second}).Do(req)
				if err != nil {
					t.Fatalf("%s MCP route failed", method)
				}
				responseBody := e2eboot.ReadBody(t, resp)
				resp.Body.Close()
				// Check side effects even when the route returns an unexpected status.
				if tc.target != "platform" {
					afterSettings, err := platformSettings.Get(ctx)
					if err != nil || !bytes.Equal(platformSettingsBefore, encode(t, afterSettings)) {
						t.Errorf("%s changed unauthorized platform settings", method)
					}
				}
				for name, target := range unrelatedSettings {
					afterSettings, err := target.Get(ctx)
					if err != nil || !bytes.Equal(unrelatedSettingsBefore[name], encode(t, afterSettings)) {
						t.Errorf("%s changed unrelated %s settings", method, name)
					}
				}
				if tc.target == "" {
					afterSettings, err := settings.Get(ctx)
					if err != nil || !bytes.Equal(encode(t, originalSettings), encode(t, afterSettings)) {
						t.Errorf("denied %s changed team web-tool settings", method)
					}
				}
				if tc.target == "platform" {
					globalAfter := secretSnapshot(t)
					if !bytes.Equal(withoutTavilySecret(t, globalBefore), withoutTavilySecret(t, globalAfter)) {
						t.Errorf("%s changed unrelated global secrets", method)
					}
					var globalRows map[string][]byte
					if err := json.Unmarshal(globalAfter, &globalRows); err != nil {
						t.Fatal("cannot inspect platform secret persistence")
					}
					if _, present := globalRows[globalKey]; present != (method == http.MethodPost) {
						t.Errorf("%s did not persist/remove the authorized global secret row", method)
					}
					wantSecret := scopedCanary
					if method == http.MethodDelete {
						wantSecret = ""
					}
					if backend.SecretGetter()(globalKey) != wantSecret {
						t.Errorf("%s did not apply the authorized Tavily global secret effect", method)
					}
					if method == http.MethodPost {
						if !bytes.Equal(legacyBefore, legacySnapshot(t)) {
							t.Error("platform install changed legacy credentials")
						}
					} else if !bytes.Equal(legacyOtherBefore, legacyContents(t, false)) {
						t.Error("platform uninstall did not preserve legacy credentials except Tavily fallback removal")
					}
				} else if !bytes.Equal(globalBefore, secretSnapshot(t)) || !bytes.Equal(legacyBefore, legacySnapshot(t)) || backend.SecretGetter()(globalKey) != wantGlobal {
					t.Errorf("%s changed global credentials", method)
				}
				if strings.Contains(responseBody, scopedCanary) || strings.Contains(responseBody, globalCanary) {
					t.Errorf("%s response exposed a synthetic secret", method)
				}
				for name, after := range snapshot(t) {
					if name != tc.target && !bytes.Equal(before[name], after) {
						t.Errorf("%s changed unauthorized %s MCP configuration", method, name)
					}
				}
				if resp.StatusCode != tc.want {
					t.Errorf("%s status = %d, want %d (body withheld)", method, resp.StatusCode, tc.want)
					continue
				}
				if tc.target == "" {
					continue
				}
				persisted, err := localMCPCanarySnapshot(ctx, stores[tc.target], "tavily")
				if err != nil {
					t.Fatal("cannot verify scoped MCP persistence")
				}
				if method == http.MethodPost {
					if persisted == nil || persisted.Env["TAVILY_API_KEY"] != scopedCanary || persisted.Transport != "stdio" || persisted.CreatedBy != actor.ID {
						t.Error("successful install did not persist authenticated scoped configuration")
					}
				} else if persisted != nil {
					t.Error("successful uninstall left scoped MCP configuration behind")
				}
			}
		})
	}
}

func hasToolInList(t *testing.T, h *e2eboot.Harness, toolName string) bool {
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

func dumpToolList(t *testing.T, h *e2eboot.Harness) string {
	t.Helper()
	resp := h.Get(t, "/api/tools")
	defer resp.Body.Close()
	b, _ := json.Marshal(struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body,omitempty"`
	}{
		Status: resp.StatusCode,
	})
	return string(b)
}

// findEnvForServer walks the loosely-typed servers-list payload and returns
// the value of envKey for the server whose name matches serverName. Returns
// "" if not found. Tolerates both array-of-server and {servers:[...]} shapes
// so the test does not break on minor API envelope changes.
func findEnvForServer(payload any, serverName, envKey string) string {
	visit := func(servers []any) string {
		for _, s := range servers {
			m, ok := s.(map[string]any)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name != serverName {
				continue
			}
			env, ok := m["env"].(map[string]any)
			if !ok {
				continue
			}
			v, _ := env[envKey].(string)
			return v
		}
		return ""
	}

	switch v := payload.(type) {
	case []any:
		return visit(v)
	case map[string]any:
		if arr, ok := v["servers"].([]any); ok {
			if found := visit(arr); found != "" {
				return found
			}
		}
		// Some endpoints return a map keyed by server name.
		if m, ok := v[serverName].(map[string]any); ok {
			if env, ok := m["env"].(map[string]any); ok {
				if val, ok := env[envKey].(string); ok {
					return val
				}
			}
		}
	}
	return ""
}
