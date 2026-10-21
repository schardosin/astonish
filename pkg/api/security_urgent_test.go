package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

// TestSecurityUrgentProviderURLChangeRequiresNewKey proves that redirecting a
// provider's destination URL while carrying its secret over masked (retained)
// is rejected — a key cannot be silently pointed at a new endpoint.
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
