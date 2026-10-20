package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/config"
	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

func TestStandardServerGlobalSecretWriteBoundary(t *testing.T) {
	for _, scope := range []string{"", "team", "org", "effective", "platform"} {
		for _, admin := range []bool{false, true} {
			secrets := newMockPlatformSecrets()
			secrets.secrets["web_servers.tavily.api_key"] = "global"
			prev := platformSecretsInstance
			platformSecretsInstance = secrets
			r := platformRequestWithScopes(t, "POST", "/?scope="+scope, nil, nil, nil)
			u := &PlatformUser{ID: "u", Role: "admin"}
			if admin {
				u.PlatformRole = "superadmin"
			}
			r = r.WithContext(WithPlatformUser(r.Context(), u))
			persistStandardServerGlobalSecret(r, config.GetStandardServer("tavily"), map[string]string{"TAVILY_API_KEY": "replacement"})
			platformSecretsInstance = prev
			want := "global"
			if admin && scope == "platform" {
				want = "replacement"
			}
			if got := secrets.GetSecret("web_servers.tavily.api_key"); got != want {
				t.Fatalf("scope=%q admin=%v got=%q", scope, admin, got)
			}
		}
	}
}

func TestMCPWriteMissingStoreDoesNotFallback(t *testing.T) {
	for _, scope := range []string{"", "team", "org", "platform"} {
		svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: newMockMCPStore(), MCPServers: newMockMCPStore(), PlatformMCPServers: newMockMCPStore()}
		switch scope {
		case "", "team":
			svc.TeamMCPServers = nil
		case "org":
			svc.MCPServers = nil
		case "platform":
			svc.PlatformMCPServers = nil
		}
		r := httptest.NewRequest("POST", "/", nil)
		r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "u", Role: "admin", PlatformRole: "superadmin"}))
		w := httptest.NewRecorder()
		if got := resolveMCPStoreForWrite(w, r, svc, scope); got != nil || w.Code != 503 {
			t.Fatalf("scope=%q status=%d", scope, w.Code)
		}
	}
}

func TestMCPWriteScopeBoundary(t *testing.T) {
	for _, tc := range []struct {
		scope, role, platform string
		status                int
		target                string
	}{
		{"", "admin", "", 200, "team"}, {"team", "admin", "", 200, "team"},
		{"org", "admin", "", 200, "org"}, {"platform", "member", "superadmin", 200, "platform"},
		{"platform", "admin", "", 403, ""}, {"org", "member", "", 403, ""},
		{"team", "member", "", 403, ""}, {"effective", "admin", "", 400, ""},
		{"typo", "admin", "superadmin", 400, ""},
	} {
		t.Run(tc.scope+tc.role+tc.platform, func(t *testing.T) {
			team, org, platform := newMockMCPStore(), newMockMCPStore(), newMockMCPStore()
			svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform}
			r := httptest.NewRequest("POST", "/?scope="+tc.scope, nil)
			r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "u", Role: tc.role, PlatformRole: tc.platform}))
			w := httptest.NewRecorder()
			got := resolveMCPStoreForWrite(w, r, svc, tc.scope)
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			targets := map[string]store.MCPServerStore{"team": team, "org": org, "platform": platform}
			if got != targets[tc.target] {
				t.Fatalf("wrong target for %q", tc.scope)
			}
		})
	}
}

func TestMCPInstallRejectsBeforeBodyOrDiscovery(t *testing.T) {
	for name, handler := range map[string]http.HandlerFunc{"internet": InternetMCPInstallHandler, "catalog": InstallMCPStoreServerHandler, "standard": InstallStandardServerHandler, "uninstall": UninstallStandardServerHandler, "create": CreateMCPPlatformServerHandler, "update": UpdateMCPPlatformServerHandler, "delete": DeleteMCPPlatformServerHandler, "patch": ToggleMCPPlatformServerHandler, "refresh": RefreshMCPPlatformServerHandler} {
		t.Run(name, func(t *testing.T) {
			svc := &store.Services{Mode: store.ModePlatform, PlatformMCPServers: newMockMCPStore()}
			r := httptest.NewRequest("POST", "/?scope=platform", strings.NewReader("invalid"))
			r = mux.SetURLVars(r, map[string]string{"id": "tavily"})
			r = r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "u", Role: "admin"}))
			w := httptest.NewRecorder()
			handler(w, r)
			if w.Code != 403 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
		})
	}
}

func TestStandardServerNarrowUninstallPreservesGlobalSecret(t *testing.T) {
	for _, scope := range []string{"", "team", "org"} {
		t.Run(scope, func(t *testing.T) {
			secrets := newMockPlatformSecrets()
			secrets.secrets["web_servers.tavily.api_key"] = "global"
			prev := platformSecretsInstance
			platformSecretsInstance = secrets
			defer func() { platformSecretsInstance = prev }()
			team, org, platform := newMockMCPStore(), newMockMCPStore(), newMockMCPStore(&store.MCPServer{Name: "tavily"})
			r := platformRequestWithScopes(t, "DELETE", "/?scope="+scope, org, team, platform)
			r = r.WithContext(WithPlatformUser(r.Context(), &PlatformUser{ID: "u", Role: "admin"}))
			r = mux.SetURLVars(r, map[string]string{"id": "tavily"})
			w := httptest.NewRecorder()
			UninstallStandardServerHandler(w, r)
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			if secrets.GetSecret("web_servers.tavily.api_key") != "global" || len(platform.deleted) > 0 {
				t.Fatal("narrow uninstall mutated platform state")
			}
			want := team
			if scope == "org" {
				want = org
			}
			if len(want.deleted) != 1 {
				t.Fatal("wrong scoped store deleted")
			}
		})
	}
}
