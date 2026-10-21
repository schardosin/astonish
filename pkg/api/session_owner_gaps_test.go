package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

// These stores model the output of tenant middleware: the same team slug in
// different organizations must still produce disjoint request-scoped stores.
// This is handler coverage, not a substitute for the real DB routing E2E.
func TestRequireSessionIDOwnerSameSlugForeignScope(t *testing.T) {
	t.Parallel()
	for _, role := range []string{"member", "superadmin"} {
		for _, personal := range []bool{true, false} {
			name := role + "/team"
			if personal {
				name = role + "/personal"
			}
			t.Run(name, func(t *testing.T) {
				// Deliberately use the same user ID in both orgs: ownership alone
				// cannot distinguish these scopes, nor may the admin override.
				requests := make([]*http.Request, 2)
				ids := []string{"acme-session", "other-org-session"}
				for i, org := range []string{"acme", "other-org"} {
					ss := &memSessionStore{metas: map[string]*store.SessionMeta{
						ids[i]: {ID: ids[i], UserID: "same-user", AppName: studioChatAppName},
					}}
					svc := &store.Services{Mode: store.ModePlatform, Sessions: &memSessionStore{}, PersonalSessions: &memSessionStore{}}
					if personal {
						svc.PersonalSessions = ss
					} else {
						svc.Sessions = ss
					}
					r := httptest.NewRequest(http.MethodGet, "/", nil)
					ctx := store.WithTenantContext(r.Context(), &store.TenantContext{OrgSlug: org, TeamSlug: "collision", UserID: "same-user"})
					ctx = WithPlatformUser(ctx, &PlatformUser{ID: "same-user", OrgSlug: org, TeamSlug: "collision", PlatformRole: role})
					requests[i] = r.WithContext(store.WithServices(ctx, svc))
				}
				for i, req := range requests {
					for _, id := range []string{ids[i], ids[1-i], "missing-session", ids[i]} {
						w := httptest.NewRecorder()
						wantAllowed := id == ids[i]
						wantStatus := http.StatusNotFound
						if wantAllowed {
							wantStatus = http.StatusOK
						}
						if got := requireSessionIDOwner(w, req, id); got != wantAllowed || w.Code != wantStatus {
							t.Fatalf("scope %d session %q: allowed=%v status=%d, want allowed=%v status=%d", i, id, got, w.Code, wantAllowed, wantStatus)
						}
					}
				}
			})
		}
	}
}

func TestNetworkSessionEndpointsRejectUnresolvedSession(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		body    string
	}{
		{
			name:    "deny network grant",
			handler: NetworkGrantDenyHandler,
			method:  http.MethodPost,
			body:    `{}`,
		},
		{
			name:    "list network denials",
			handler: NetworkDenialCheckHandler,
			method:  http.MethodGet,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/api/studio/sessions/foreign/network", bytes.NewBufferString(tt.body))
			req = mux.SetURLVars(req, map[string]string{"id": "foreign-session"})
			w := httptest.NewRecorder()

			tt.handler(w, req)

			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 before sandbox or gateway access: %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestArtifactSessionVisibleRejectsNoSandboxWithoutOwnedSession(t *testing.T) {
	registry, err := sandbox.NewLocalSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalSessionStore: %v", err)
	}
	reg := sandbox.NewSessionRegistryFromStore(registry)
	svc := &store.Services{Mode: store.ModePlatform, PersonalSessions: &memSessionStore{metas: map[string]*store.SessionMeta{
		"owned-session":   {ID: "owned-session", UserID: "owner", AppName: studioChatAppName},
		"foreign-session": {ID: "foreign-session", UserID: "other", AppName: studioChatAppName},
	}}}
	for _, tc := range []struct {
		id      string
		allowed bool
	}{
		{id: "owned-session", allowed: true},
		{id: "foreign-session"},
		{id: "missing-session"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(WithPlatformUser(store.WithServices(req.Context(), svc), &PlatformUser{ID: "owner"}))
			req = withSandboxRegistry(req, reg)
			w := httptest.NewRecorder()
			if got := artifactSessionVisible(w, req, tc.id); got != tc.allowed {
				t.Fatalf("visible = %v, want %v", got, tc.allowed)
			}
			wantStatus := http.StatusNotFound
			if tc.allowed {
				wantStatus = http.StatusOK
			}
			if w.Code != wantStatus {
				t.Fatalf("status = %d, want %d", w.Code, wantStatus)
			}
		})
	}
}
