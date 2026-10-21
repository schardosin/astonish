package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

const mcpTestURL = "https://user:password@service.invalid/mcp?token=secret-one&token=secret-two#fragment-secret"

func mcpRedactionFixture() *store.MCPServer {
	disabled := false
	return &store.MCPServer{Name: "private-server", Command: "server", Args: []string{"--safe"}, Transport: "sse", URL: mcpTestURL, Env: map[string]string{"ARBITRARY": "literal-secret", "EMPTY": "", "REF": "{{CREDENTIAL:github:token}}", "MIXED": "prefix-secret{{CREDENTIAL:github:token}}"}, Enabled: &disabled, CachedTools: json.RawMessage(`[]`)}
}

type mcpRedactionStore struct {
	*mockMCPServerStore
	saves int
}

func (s *mcpRedactionStore) Save(ctx context.Context, server *store.MCPServer) error {
	s.saves++
	return s.mockMCPServerStore.Save(ctx, server)
}
func mcpSecretRequest(method, scope, name, body string, team, org, platform store.MCPServerStore) *http.Request {
	r := httptest.NewRequest(method, "/?scope="+scope, strings.NewReader(body))
	r = mux.SetURLVars(r, map[string]string{"name": name})
	svc := &store.Services{Mode: store.ModePlatform, TeamMCPServers: team, MCPServers: org, PlatformMCPServers: platform}
	return r.WithContext(WithPlatformUser(store.WithServices(r.Context(), svc), &PlatformUser{ID: "admin", Role: "admin", PlatformRole: "superadmin"}))
}

func TestMCPRedactionResponseCopies(t *testing.T) {
	original := mcpRedactionFixture()
	item := mcpServerToListItem(original, "team")
	if item.Env["ARBITRARY"] != "****" || item.Env["MIXED"] != "****" || item.Env["EMPTY"] != "" || item.Env["REF"] != original.Env["REF"] {
		t.Fatalf("environment was not safely redacted")
	}
	if item.URL != redactBrowserURL(original.URL) {
		t.Fatal("URL was not redacted")
	}
	item.Env["ARBITRARY"] = "changed-response"
	if original.Env["ARBITRARY"] != "literal-secret" || original.URL != mcpTestURL {
		t.Fatal("response mutated runtime configuration")
	}
	for _, raw := range []string{"https://service.invalid/?credential=unusual-secret", "https://user:pw@service.invalid", "https://service.invalid/#fragment-secret", "https://service.invalid/?bad=%zz", "not-a-url-secret"} {
		original.URL = raw
		if got := mcpServerToListItem(original, "team").URL; got == raw {
			t.Fatal("URL secret retained")
		}
	}
}

func TestMCPRedactionReadHandlers(t *testing.T) {
	for _, scope := range []string{"team", "org", "platform", ""} {
		for _, get := range []bool{false, true} {
			t.Run(scope+map[bool]string{true: "/get", false: "/list"}[get], func(t *testing.T) {
				s := mcpRedactionFixture()
				backing := newMockMCPStore(s)
				r := mcpSecretRequest("GET", scope, s.Name, "", backing, backing, backing)
				w := httptest.NewRecorder()
				if get {
					GetMCPPlatformServerHandler(w, r)
				} else {
					ListMCPPlatformServersHandler(w, r)
				}
				if w.Code != 200 {
					t.Fatalf("status %d", w.Code)
				}
				for _, secret := range []string{"literal-secret", "prefix-secret", "password", "secret-one", "secret-two", "fragment-secret"} {
					if strings.Contains(w.Body.String(), secret) {
						t.Fatalf("response leaked %s", secret)
					}
				}
				if s.Env["ARBITRARY"] != "literal-secret" || s.URL != mcpTestURL {
					t.Fatal("read changed stored secrets")
				}
			})
		}
	}
}

func TestMCPRedactionMaskedUpdate(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		status int
	}{
		{"same target", func(m map[string]any) {}, 200},
		{"missing env", func(m map[string]any) { m["env"] = map[string]string{"ABSENT": "****"} }, 400},
		{"different command", func(m map[string]any) { m["command"] = "untrusted" }, 400},
		{"different args", func(m map[string]any) { m["args"] = []string{"--untrusted"} }, 400},
		{"different transport", func(m map[string]any) { m["transport"] = "stdio" }, 400},
		{"different URL", func(m map[string]any) { m["url"] = "https://attacker.invalid/mcp" }, 400},
		{"masked URL different host", func(m map[string]any) {
			m["url"] = strings.Replace(redactBrowserURL(mcpTestURL), "service.invalid", "attacker.invalid", 1)
		}, 400},
		{"rename", func(m map[string]any) { m["name"] = "new-server" }, 400},
		{"replacement", func(m map[string]any) {
			m["command"] = "new-server"
			m["env"] = map[string]string{"ARBITRARY": "replacement"}
			m["url"] = "https://replacement.invalid/mcp"
		}, 200},
		{"explicit empty", func(m map[string]any) { m["env"] = map[string]string{}; m["url"] = "" }, 200},
		{"omitted", func(m map[string]any) {
			for k := range m {
				delete(m, k)
			}
			m["enabled"] = false
		}, 200},
		{"omitted secrets changed command", func(m map[string]any) { delete(m, "env"); delete(m, "url"); m["command"] = "untrusted" }, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := mcpRedactionFixture()
			target := &mcpRedactionStore{mockMCPServerStore: newMockMCPStore(existing)}
			body := map[string]any{"name": existing.Name, "command": existing.Command, "args": existing.Args, "transport": existing.Transport, "url": redactBrowserURL(existing.URL), "env": map[string]string{"ARBITRARY": "****", "EMPTY": "", "REF": existing.Env["REF"], "MIXED": "****"}, "enabled": false}
			tc.change(body)
			encoded, _ := json.Marshal(body)
			// In-flight cache suppresses all background work, including on the pre-fix path.
			discoveryInFlight.Store(existing.Name, struct{}{})
			discoveryInFlight.Store("new-server", struct{}{})
			defer discoveryInFlight.Delete(existing.Name)
			defer discoveryInFlight.Delete("new-server")
			w := httptest.NewRecorder()
			UpdateMCPPlatformServerHandler(w, mcpSecretRequest("PUT", "team", existing.Name, string(encoded), target, nil, nil))
			if w.Code != tc.status {
				t.Fatalf("status=%d want=%d body=%s", w.Code, tc.status, w.Body.String())
			}
			if tc.status != 200 {
				if target.saves != 0 {
					t.Fatal("invalid masks persisted")
				}
				return
			}
			if target.saves != 1 {
				t.Fatal("update was not saved")
			}
			saved := target.servers[existing.Name]
			if tc.name == "same target" || tc.name == "omitted" {
				if saved.URL != existing.URL || !reflect.DeepEqual(saved.Env, existing.Env) {
					t.Fatal("stored secrets were not restored")
				}
			}
			if tc.name == "explicit empty" && (saved.URL != "" || len(saved.Env) != 0) {
				t.Fatal("explicit empty did not clear")
			}
			if tc.name == "replacement" && (saved.Env["ARBITRARY"] != "replacement" || saved.URL != "https://replacement.invalid/mcp") {
				t.Fatal("replacement not saved")
			}
		})
	}
}

func TestMCPRedactionRestoresExactScope(t *testing.T) {
	for _, scope := range []string{"team", "org", "platform"} {
		t.Run(scope, func(t *testing.T) {
			makeStore := func(secret string) *mcpRedactionStore {
				server := mcpRedactionFixture()
				server.Env = map[string]string{"TOKEN": secret}
				return &mcpRedactionStore{mockMCPServerStore: newMockMCPStore(server)}
			}
			team, org, platform := makeStore("team-secret"), makeStore("org-secret"), makeStore("platform-secret")
			discoveryInFlight.Store("private-server", struct{}{})
			defer discoveryInFlight.Delete("private-server")
			w := httptest.NewRecorder()
			UpdateMCPPlatformServerHandler(w, mcpSecretRequest("PUT", scope, "private-server", `{"env":{"TOKEN":"****"},"enabled":false}`, team, org, platform))
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			for name, backing := range map[string]*mcpRedactionStore{"team": team, "org": org, "platform": platform} {
				wantSaves := 0
				if name == scope {
					wantSaves = 1
				}
				if backing.saves != wantSaves || backing.servers["private-server"].Env["TOKEN"] != name+"-secret" {
					t.Fatal("wrong scope restored or saved")
				}
			}
		})
	}
}

func TestMCPRedactionRestoreHelper(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*store.MCPServer, *store.MCPServer)
		wantError bool
	}{
		{"URL only retained changed executable", func(u, e *store.MCPServer) { u.Env = map[string]string{}; u.Command = "untrusted" }, true},
		{"empty stored env", func(u, e *store.MCPServer) { u.Env = map[string]string{"EMPTY": "****"} }, true},
		{"reference is not a mask source", func(u, e *store.MCPServer) { u.Env = map[string]string{"REF": "****"} }, true},
		{"partial mask", func(u, e *store.MCPServer) { u.Env = map[string]string{"ARBITRARY": "****cret"} }, true},
		{"omitted reference changed executable", func(u, e *store.MCPServer) {
			e.Env = map[string]string{"REF": "{{CREDENTIAL:github:token}}"}
			e.URL = ""
			u.URL = ""
			u.Command = "untrusted"
			u.Env = nil
		}, true},
		{"explicit empty key", func(u, e *store.MCPServer) { u.Env = map[string]string{"ARBITRARY": ""} }, false},
		{"URL percent encoding preserved", func(u, e *store.MCPServer) {
			e.URL = "https://service.invalid/mcp?b=secret%2fone&a=secret+two&b=secret%2Fthree"
			u.URL = redactBrowserURL(e.URL)
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			existing := mcpRedactionFixture()
			updated := *existing
			updated.Env = redactMCPEnv(existing.Env)
			updated.URL = redactBrowserURL(existing.URL)
			tc.change(&updated, existing)
			beforeUpdated, _ := json.Marshal(updated)
			beforeExisting, _ := json.Marshal(existing)
			err := restoreMCPServerSecrets(&updated, existing)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v wantError=%v", err, tc.wantError)
			}
			afterExisting, _ := json.Marshal(existing)
			if string(afterExisting) != string(beforeExisting) {
				t.Fatal("helper mutated stored value")
			}
			if err != nil {
				afterUpdated, _ := json.Marshal(updated)
				if string(afterUpdated) != string(beforeUpdated) {
					t.Fatal("rejected update was partially restored")
				}
			} else if updated.URL != existing.URL {
				t.Fatal("original URL bytes not preserved")
			}
		})
	}
	for _, field := range []string{"env", "url"} {
		u := &store.MCPServer{Name: "new"}
		if field == "env" {
			u.Env = map[string]string{"X": "****"}
		} else {
			u.URL = redactBrowserURL(mcpTestURL)
		}
		if restoreMCPServerSecrets(u, nil) == nil {
			t.Fatalf("create accepted %s mask", field)
		}
	}
}

func TestMCPRedactionRejectsCrossScopeAndCreateMasks(t *testing.T) {
	for _, method := range []string{"POST", "PUT"} {
		for _, scope := range []string{"team", "org", "platform"} {
			t.Run(method+scope, func(t *testing.T) {
				inherited := newMockMCPStore(mcpRedactionFixture())
				target := &mcpRedactionStore{mockMCPServerStore: newMockMCPStore()}
				team, org, platform := store.MCPServerStore(inherited), store.MCPServerStore(inherited), store.MCPServerStore(inherited)
				switch scope {
				case "team":
					team = target
				case "org":
					org = target
				case "platform":
					platform = target
				}
				discoveryInFlight.Store("private-server", struct{}{})
				defer discoveryInFlight.Delete("private-server")
				r := mcpSecretRequest(method, scope, "private-server", `{"name":"private-server","command":"server","transport":"stdio","env":{"ARBITRARY":"****"},"enabled":false}`, team, org, platform)
				w := httptest.NewRecorder()
				if method == "POST" {
					CreateMCPPlatformServerHandler(w, r)
				} else {
					UpdateMCPPlatformServerHandler(w, r)
				}
				if w.Code != 400 || target.saves != 0 {
					t.Fatalf("status=%d saves=%d", w.Code, target.saves)
				}
			})
		}
	}
}
