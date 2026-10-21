package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

type urgentSettingsStore struct {
	value *store.TeamSettings
	saves int
}

func (s *urgentSettingsStore) Get(context.Context) (*store.TeamSettings, error) { return s.value, nil }
func (s *urgentSettingsStore) Save(_ context.Context, v *store.TeamSettings) error {
	s.value = v
	s.saves++
	return nil
}

func TestSecurityUrgentSettingsMasksResponseOnly(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s := &urgentSettingsStore{value: &store.TeamSettings{Providers: map[string]map[string]string{
		"openai": {"type": "openai", "api_key": "canary-openai-secret"},
		"sap":    {"type": "sap_ai_core", "client_secret": "canary-sap-secret"},
		"oauth":  {"type": "xai_oauth", "access_token": "canary-access-secret", "refresh_token": "canary-refresh-secret"},
	}}}
	r := httptest.NewRequest("GET", "/api/settings/config", nil)
	r = r.WithContext(store.WithServices(r.Context(), &store.Services{Mode: store.ModePlatform, Settings: s}))
	w := httptest.NewRecorder()
	GetSettingsHandler(w, r)
	if w.Code != 200 || strings.Contains(w.Body.String(), "canary-") {
		t.Fatalf("unmasked response: %d %s", w.Code, w.Body)
	}
	if s.value.Providers["openai"]["api_key"] != "canary-openai-secret" {
		t.Fatal("response masking mutated source")
	}
	var result AppSettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Providers) != 3 {
		t.Fatalf("providers: %d", len(result.Providers))
	}
	for _, p := range result.Providers {
		if !p.Configured {
			t.Errorf("%s lost configured state", p.Name)
		}
	}
}

func TestSecurityUrgentMaskedUpdatesSameTarget(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "replacement"}[replacement], func(t *testing.T) {
			s := &urgentSettingsStore{value: &store.TeamSettings{Providers: map[string]map[string]string{"existing": {"type": "openai", "api_key": "original-secret"}}}}
			req := UpdateAppSettingsRequest{Providers: map[string]map[string]string{"existing": {"api_key": "****cret"}}}
			if replacement {
				req.Providers = map[string]map[string]string{"__replace_all__": {"__array__": `[{"name":"existing","type":"openai","api_key":"****cret"}]`}}
			}
			w := httptest.NewRecorder()
			updateSettingsPlatform(w, httptest.NewRequest("PUT", "/api/settings/config", nil), &store.Services{Settings: s}, req)
			if w.Code != 200 || s.saves != 1 {
				t.Fatalf("save: %d %s", w.Code, w.Body)
			}
			if s.value.Providers["existing"]["api_key"] != "original-secret" || s.value.Providers["new"]["api_key"] != "" {
				t.Fatal("masked update lost or copied secret across targets")
			}
		})
	}
}

func TestSecurityUrgentSettingsRejectInvalidWithoutMutation(t *testing.T) {
	for _, providers := range []map[string]map[string]string{
		{"new": {"api_key": "****cret"}},
		{"__replace_all__": {"__array__": `[{"name":"new","api_key":"****cret"}]`}},
		{"__replace_all__": {"__array__": `invalid`}},
	} {
		original := &store.TeamSettings{DefaultModel: "before", Providers: map[string]map[string]string{"existing": {"api_key": "original-secret"}}}
		s := &urgentSettingsStore{value: original}
		w := httptest.NewRecorder()
		updateSettingsPlatform(w, httptest.NewRequest("PUT", "/", nil), &store.Services{Settings: s}, UpdateAppSettingsRequest{General: &GeneralSettings{DefaultModel: "after"}, Providers: providers})
		if w.Code != 400 || s.saves != 0 || original.DefaultModel != "before" || len(original.Providers) != 1 {
			t.Fatalf("invalid update must have no effects: status=%d saves=%d model=%s", w.Code, s.saves, original.DefaultModel)
		}
	}
}

func TestSecurityUrgentSettingsOmitAndClear(t *testing.T) {
	original := &store.TeamSettings{Providers: map[string]map[string]string{"existing": {"api_key": "secret", "base_url": "https://example.com"}}}
	s := &urgentSettingsStore{value: original}
	w := httptest.NewRecorder()
	updateSettingsPlatform(w, httptest.NewRequest("PUT", "/", nil), &store.Services{Settings: s}, UpdateAppSettingsRequest{Providers: map[string]map[string]string{"existing": {"api_key": ""}}})
	if w.Code != 200 || s.value.Providers["existing"]["api_key"] != "" || s.value.Providers["existing"]["base_url"] != "https://example.com" || original.Providers["existing"]["api_key"] != "secret" {
		t.Fatal("explicit deletion, omission, or copy isolation failed")
	}
}

func TestSecurityUrgentLegacyMCPEndpointsAuthorizeBeforeBody(t *testing.T) {
	for _, handler := range []http.HandlerFunc{UpdateMCPSettingsHandler, InstallInlineMCPServerHandler} {
		r := httptest.NewRequest("PUT", "/?scope=platform", strings.NewReader("invalid"))
		r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), &store.Services{Mode: store.ModePlatform, PlatformMCPServers: newMockMCPStore()}), &PlatformUser{ID: "u", Role: "admin"}))
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("got %d want 403", w.Code)
		}
	}
}

func TestSecurityUrgentMCPSettingsSecretRoundTrip(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		t.Run(map[bool]string{false: "roundtrip", true: "invalid-before-delete"}[invalid], func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			original := mcpRedactionFixture()
			target := &mcpRedactionStore{mockMCPServerStore: newMockMCPStore(original, &store.MCPServer{Name: "removed"})}
			r := mcpSecretRequest("GET", "team", "", "", target, nil, nil)
			w := httptest.NewRecorder()
			GetMCPSettingsHandler(w, r)
			if w.Code != 200 || strings.Contains(w.Body.String(), "literal-secret") || strings.Contains(w.Body.String(), "secret-one") {
				t.Fatalf("unsafe settings response: %d", w.Code)
			}
			var cfg config.MCPConfig
			if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
				t.Fatal(err)
			}
			delete(cfg.MCPServers, "removed")
			if invalid {
				entry := cfg.MCPServers[original.Name]
				entry.Env = map[string]string{"ABSENT": "****"}
				cfg.MCPServers[original.Name] = entry
			}
			discoveryInFlight.Store(original.Name, struct{}{})
			defer discoveryInFlight.Delete(original.Name)
			w = httptest.NewRecorder()
			updateMCPSettingsPlatform(w, r, target, cfg)
			if invalid {
				if w.Code != 400 || target.saves != 0 || len(target.deleted) != 0 {
					t.Fatalf("invalid settings caused effects: %d saves=%d deletes=%d", w.Code, target.saves, len(target.deleted))
				}
			} else if w.Code != 200 || target.servers[original.Name].Env["ARBITRARY"] != "literal-secret" || target.servers[original.Name].URL != mcpTestURL {
				t.Fatalf("roundtrip lost secrets: %d", w.Code)
			}
		})
	}
}

// TestSecurityUrgentMCPSettingsSecretArgsRoundTrip exercises updateMCPSettingsPlatform
// end-to-end for servers whose SECRET travels in Command/Args (not Env/URL). This is
// the exact path that previously leaked: GetMCPSettingsHandler masks "--api-key sk-real"
// to "--api-key ****", the UI echoes that masked view back on PUT, and the save must
// restore the original argv instead of persisting the "****" mask over the real key.
// The DB-server and config-file-server subcases cover both branches of the settings save.
func TestSecurityUrgentMCPSettingsSecretArgsRoundTrip(t *testing.T) {
	t.Run("db-server", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		disabled := false
		// A DB-backed server whose real API key lives in its argv, both split
		// ("--api-key", "sk-db-realkey") and inline ("--token=sk-db-inline").
		original := &store.MCPServer{
			Name:      "db-secret-args",
			Command:   "npx",
			Args:      []string{"server", "--api-key", "sk-db-realkey", "--token=sk-db-inline", "--safe"},
			Transport: "sse",
			URL:       mcpTestURL,
			Env:       map[string]string{"ARBITRARY": "literal-secret"},
			Enabled:   &disabled,
		}
		target := &mcpRedactionStore{mockMCPServerStore: newMockMCPStore(original)}

		// GET: the Settings UI reads the redacted view.
		r := mcpSecretRequest("GET", "team", "", "", target, nil, nil)
		w := httptest.NewRecorder()
		GetMCPSettingsHandler(w, r)
		body := w.Body.String()
		if w.Code != 200 || strings.Contains(body, "sk-db-realkey") || strings.Contains(body, "sk-db-inline") || strings.Contains(body, "literal-secret") {
			t.Fatalf("GET leaked a secret or failed: code=%d body=%s", w.Code, body)
		}
		var cfg config.MCPConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}
		// Sanity: the echoed argv really is masked, so a plain copy WOULD persist "****".
		if sent := cfg.MCPServers[original.Name]; !containsString(sent.Args, browserURLMask) {
			t.Fatalf("expected masked args in GET echo, got %v", sent.Args)
		}

		// PUT: echo the masked config straight back, as the UI does.
		discoveryInFlight.Store(original.Name, struct{}{})
		defer discoveryInFlight.Delete(original.Name)
		w = httptest.NewRecorder()
		updateMCPSettingsPlatform(w, r, target, cfg)
		if w.Code != 200 {
			t.Fatalf("save failed: code=%d body=%s", w.Code, w.Body.String())
		}
		saved := target.servers[original.Name]
		if saved == nil {
			t.Fatal("server row not saved")
		}
		// The restored argv must equal the ORIGINAL (real) argv, never the mask.
		if !reflect.DeepEqual(saved.Args, original.Args) {
			t.Fatalf("args not restored: got %v want %v", saved.Args, original.Args)
		}
		if containsString(saved.Args, browserURLMask) {
			t.Fatalf("mask persisted over real argv: %v", saved.Args)
		}
		if saved.Env["ARBITRARY"] != "literal-secret" || saved.URL != mcpTestURL {
			t.Fatalf("env/url secrets lost on save: env=%v url=%q", saved.Env, saved.URL)
		}
	})

	t.Run("config-file-server", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		// A config-file (mcp_config.json) server with a secret in its args. On an
		// unchanged save the UI echoes the masked view; the "unchanged echo" check
		// in updateMCPSettingsPlatform must recognize it as the file default (by
		// masking the file's Command/Args too) and NOT persist a DB override — and
		// must NOT fail the save with a 400 when the secret restore cannot match.
		fileCfg := &config.MCPConfig{MCPServers: map[string]config.MCPServerConfig{
			"file-secret-args": {
				Command:   "npx",
				Args:      []string{"file-server", "--api-key", "sk-file-realkey"},
				Transport: "sse",
			},
		}}
		if dir, err := config.GetConfigDir(); err != nil {
			t.Fatal(err)
		} else if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := config.SaveMCPConfig(fileCfg); err != nil {
			t.Fatal(err)
		}
		target := &mcpRedactionStore{mockMCPServerStore: newMockMCPStore()}

		r := mcpSecretRequest("GET", "team", "", "", target, nil, nil)
		w := httptest.NewRecorder()
		GetMCPSettingsHandler(w, r)
		body := w.Body.String()
		if w.Code != 200 || strings.Contains(body, "sk-file-realkey") {
			t.Fatalf("GET leaked file secret or failed: code=%d body=%s", w.Code, body)
		}
		var cfg config.MCPConfig
		if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
			t.Fatal(err)
		}

		// PUT: echo the masked file-server config back unchanged.
		w = httptest.NewRecorder()
		updateMCPSettingsPlatform(w, r, target, cfg)
		if w.Code != 200 {
			t.Fatalf("unchanged file-server save failed (masked args not recognized as file default): code=%d body=%s", w.Code, w.Body.String())
		}
		// The file server is the read-only base layer: an unchanged echo must NOT
		// persist a DB row (which would have carried the masked argv forever).
		if target.saves != 0 {
			t.Fatalf("unchanged file-server echo persisted %d DB row(s)", target.saves)
		}
		if _, ok := target.servers["file-secret-args"]; ok {
			t.Fatal("unchanged file-server echo wrongly created a DB override row")
		}
	})
}

func containsString(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

type urgentUnreadBody struct{ t *testing.T }

func (b urgentUnreadBody) Read([]byte) (int, error) {
	b.t.Error("configured init read body before rejecting")
	return 0, io.EOF
}
func (urgentUnreadBody) Close() error { return nil }

func TestSecurityUrgentInitConfiguredBeforeBody(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, backend := range []string{"postgres", "sqlite"} {
		cfg := &config.AppConfig{}
		cfg.Storage.Backend = backend
		cfg.Storage.Auth.JWTSecret = "unchanged-jwt"
		if err := config.SaveAppConfig(cfg); err != nil {
			t.Fatal(err)
		}
		for _, handler := range []http.HandlerFunc{PlatformInitHandler, SQLitePlatformInitHandler} {
			r := httptest.NewRequest("POST", "/api/platform/init", nil)
			r.Body = urgentUnreadBody{t}
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != 409 {
				t.Fatalf("%s: got %d %s", backend, w.Code, w.Body)
			}
		}
		after, err := config.LoadAppConfig()
		if err != nil {
			t.Fatal(err)
		}
		if after.Storage.Backend != backend || after.Storage.Auth.JWTSecret != "unchanged-jwt" {
			t.Fatal("denied init changed config")
		}
	}
}

func TestSecurityUrgentFullConfigRolesAndLoopback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, tc := range []struct {
		name string
		user *PlatformUser
		mode store.DeploymentMode
		want int
	}{
		{"local", nil, "", 200},
		{"anonymous", nil, store.ModePlatform, 401},
		{"member", &PlatformUser{Role: "member"}, store.ModePlatform, 403},
		{"org-admin", &PlatformUser{Role: "admin"}, store.ModePlatform, 403},
		{"org-owner", &PlatformUser{Role: "owner"}, store.ModePlatform, 403},
		{"platform-admin", &PlatformUser{PlatformRole: "superadmin"}, store.ModePlatform, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.AppConfig{}
			cfg.AgentIdentity.Name = "before"
			if err := config.SaveAppConfig(cfg); err != nil {
				t.Fatal(err)
			}
			r := authzRequest(tc.user, &store.Services{Mode: tc.mode}, nil)
			r.RemoteAddr = "127.0.0.1:12345"
			r.Body = io.NopCloser(strings.NewReader(`{"agent_identity":{"name":"after"}}`))
			w := httptest.NewRecorder()
			UpdateFullConfigHandler(w, r)
			if w.Code != tc.want {
				t.Fatalf("got %d want %d: %s", w.Code, tc.want, w.Body)
			}
			after, err := config.LoadAppConfig()
			if err != nil {
				t.Fatal(err)
			}
			want := "before"
			if tc.want == 200 {
				want = "after"
			}
			if after.AgentIdentity.Name != want {
				t.Fatal("unexpected config mutation")
			}
		})
	}
}

// --- Review-driven regression tests (PR #614) ---

// TestSecurityUrgentBootstrapTokenMinLength proves a too-short operator
// bootstrap token cannot authorize first-run setup even when the provided
// header matches it exactly. A long token still authorizes.
func TestSecurityUrgentBootstrapTokenMinLength(t *testing.T) {
	for _, tc := range []struct {
		name    string
		token   string
		allowed bool
	}{
		{"empty", "", false},
		{"short", "short-token", false},
		{"exact-min", strings.Repeat("a", minBootstrapTokenLen), true},
		{"long", strings.Repeat("z", minBootstrapTokenLen+8), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ASTONISH_BOOTSTRAP_TOKEN", tc.token)
			r := httptest.NewRequest("POST", "/api/platform/init", nil)
			r.Header.Set("X-Astonish-Bootstrap-Token", tc.token)
			w := httptest.NewRecorder()
			if got := requireBootstrapAuthority(w, r); got != tc.allowed {
				t.Fatalf("authorized=%v want %v (status=%d)", got, tc.allowed, w.Code)
			}
			if !tc.allowed && w.Code != http.StatusForbidden {
				t.Fatalf("denied token status=%d want 403", w.Code)
			}
		})
	}
}

// TestSecurityUrgentMCPServerListRedactsSecrets proves GET /api/mcp/servers
// masks credential-bearing URLs and command tokens instead of returning them
// in the clear to any member.
func TestSecurityUrgentMCPServerListRedactsSecrets(t *testing.T) {
	org := newMockMCPStore(&store.MCPServer{
		Name:      "leaky",
		Transport: "stdio",
		Command:   "npx mcp-server --api-key=canary-cmd-secret",
		URL:       "https://user:canary-url-secret@mcp.invalid/path?token=canary-query-secret",
	})
	r := httptest.NewRequest("GET", "/api/mcp/servers", nil)
	r = r.WithContext(store.WithServices(r.Context(), &store.Services{Mode: store.ModePlatform, MCPServers: org}))
	w := httptest.NewRecorder()
	GetMCPServersHandler(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
	if strings.Contains(w.Body.String(), "canary-") {
		t.Fatalf("MCP server list leaked a secret: %s", w.Body)
	}
}

// TestSecurityUrgentInspectorCacheScopeGate proves the inspector's DB
// cache-write resolver returns a store only for callers authorized at the
// requested scope, so a member cannot populate the platform/org cache.
func TestSecurityUrgentInspectorCacheScopeGate(t *testing.T) {
	for _, tc := range []struct {
		name     string
		user     *PlatformUser
		scope    string
		wantNils bool
	}{
		{"member-platform", &PlatformUser{Role: "member"}, "platform", true},
		{"member-org", &PlatformUser{Role: "member"}, "org", true},
		{"org-admin-platform", &PlatformUser{Role: "admin"}, "platform", true},
		{"platform-admin-platform", &PlatformUser{PlatformRole: "superadmin"}, "platform", false},
		{"org-admin-org", &PlatformUser{Role: "admin"}, "org", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := &store.Services{
				Mode:               store.ModePlatform,
				PlatformMCPServers: newMockMCPStore(),
				MCPServers:         newMockMCPStore(),
				TeamMCPServers:     newMockMCPStore(),
			}
			r := httptest.NewRequest("GET", "/api/mcp/srv/tools?scope="+tc.scope, nil)
			r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), tc.user))
			got := authorizedMCPStoreForInspectorCache(r)
			if (got == nil) != tc.wantNils {
				t.Fatalf("store nil=%v want nil=%v", got == nil, tc.wantNils)
			}
		})
	}
}

// TestSecurityUrgentInspectorOmittedScopeResolvesTeam proves the inspector's
// live server lookup resolves the SAME scope its authorization check uses.
// authorizeMCPInspectorScope treats an omitted ?scope as team (member-level),
// so the server resolution must also resolve the team store — otherwise a
// member who simply omits the parameter passes the team auth check but starts
// the ORG server and runs its tools with org credentials (the reviewer's gap).
func TestSecurityUrgentInspectorOmittedScopeResolvesTeam(t *testing.T) {
	teamSrv := &store.MCPServer{Name: "srv", Transport: "stdio", Command: "team-cmd"}
	orgSrv := &store.MCPServer{Name: "srv", Transport: "stdio", Command: "org-cmd"}
	svc := &store.Services{
		Mode:               store.ModePlatform,
		TeamMCPServers:     newMockMCPStore(teamSrv),
		MCPServers:         newMockMCPStore(orgSrv),
		PlatformMCPServers: newMockMCPStore(),
	}

	cases := []struct {
		name      string
		scope     string
		wantCmd   string
	}{
		{"omitted resolves team", "", "team-cmd"},
		{"explicit team", "team", "team-cmd"},
		{"explicit org", "org", "org-cmd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url := "/api/mcp/srv/tools"
			if tc.scope != "" {
				url += "?scope=" + tc.scope
			}
			r := httptest.NewRequest("GET", url, nil)
			r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "u", Role: "admin"}))
			got := inspectorMCPStore(r)
			if got == nil {
				t.Fatalf("inspectorMCPStore returned nil for scope %q", tc.scope)
			}
			srv, err := got.Get(r.Context(), "srv")
			if err != nil || srv == nil {
				t.Fatalf("server not found in resolved store: %v", err)
			}
			if srv.Command != tc.wantCmd {
				t.Fatalf("scope %q resolved wrong store: command=%q want %q", tc.scope, srv.Command, tc.wantCmd)
			}
		})
	}
}
func TestSecurityUrgentProviderURLChangeRequiresNewKey(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fields  map[string]string
		wantErr bool
	}{
		{
			name:    "url-change-retained-secret-rejected",
			fields:  map[string]string{"api_key": "****cret", "base_url": "https://evil.invalid"},
			wantErr: true,
		},
		{
			name:    "url-change-with-new-secret-ok",
			fields:  map[string]string{"api_key": "brand-new-secret", "base_url": "https://evil.invalid"},
			wantErr: false,
		},
		{
			name:    "same-url-retained-secret-ok",
			fields:  map[string]string{"api_key": "****cret", "base_url": "https://origin.invalid"},
			wantErr: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &urgentSettingsStore{value: &store.TeamSettings{Providers: map[string]map[string]string{
				"existing": {"type": "openai", "api_key": "original-secret", "base_url": "https://origin.invalid"},
			}}}
			req := UpdateAppSettingsRequest{Providers: map[string]map[string]string{"existing": tc.fields}}
			w := httptest.NewRecorder()
			updateSettingsPlatform(w, httptest.NewRequest("PUT", "/", nil), &store.Services{Settings: s}, req)
			if tc.wantErr {
				if w.Code != 400 || s.saves != 0 {
					t.Fatalf("url change w/ retained secret: status=%d saves=%d want 400/0", w.Code, s.saves)
				}
			} else if w.Code != 200 || s.saves != 1 {
				t.Fatalf("legitimate update: status=%d saves=%d want 200/1 %s", w.Code, s.saves, w.Body)
			}
		})
	}
}

// TestSecurityUrgentMemoryBySessionOwnerCheck proves the memory-by-session
// listing is gated by session ownership: foreign and missing sessions are
// denied before any memory store is touched.
func TestSecurityUrgentMemoryBySessionOwnerCheck(t *testing.T) {
	svc := &store.Services{Mode: store.ModePlatform, PersonalSessions: &memSessionStore{metas: map[string]*store.SessionMeta{
		"owned":   {ID: "owned", UserID: "owner", AppName: studioChatAppName},
		"foreign": {ID: "foreign", UserID: "other", AppName: studioChatAppName},
	}}, Sessions: &memSessionStore{}}
	for _, tc := range []struct {
		id         string
		wantDenied bool
	}{
		{"foreign", true},
		{"missing", true},
	} {
		t.Run(tc.id, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/memories/session/"+tc.id, nil)
			r = mux.SetURLVars(r, map[string]string{"id": tc.id})
			r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "owner"}))
			w := httptest.NewRecorder()
			MemoryListBySessionHandler(w, r)
			if tc.wantDenied && w.Code != http.StatusNotFound {
				t.Fatalf("session %q status=%d want 404", tc.id, w.Code)
			}
		})
	}
}

func TestSecurityUrgentProviderReadRoles(t *testing.T) {
	for _, handler := range []http.HandlerFunc{GetPlatformProvidersHandler, GetOrgProvidersHandler} {
		for _, user := range []*PlatformUser{nil, {Role: "member"}} {
			r := authzRequest(user, &store.Services{Mode: store.ModePlatform}, nil)
			w := httptest.NewRecorder()
			handler(w, r)
			want := 403
			if user == nil {
				want = 401
			}
			if w.Code != want {
				t.Fatalf("got %d want %d", w.Code, want)
			}
		}
	}
}
