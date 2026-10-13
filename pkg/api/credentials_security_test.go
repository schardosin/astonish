package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

func credentialAccessRequest(path string, scope string) *http.Request {
	if scope != "" {
		path += "?scope=" + scope
	}
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = mux.SetURLVars(req, map[string]string{"name": "team-secret"})
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{
		ID:       "member-1",
		OrgSlug:  "test-org",
		TeamSlug: "test-team",
		Role:     "member",
	}))
	return req
}

func TestGetCredential_OmittedScopeRequiresTeamAdmin(t *testing.T) {
	req := credentialAccessRequest("/api/credentials/team-secret", "")
	svc := &store.Services{Mode: store.ModePlatform, Credentials: &mockCredentialStore{}}
	req = req.WithContext(store.WithServices(req.Context(), svc))

	w := httptest.NewRecorder()
	GetCredentialHandler(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected omitted scope to require team admin, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetCredential_ExplicitPersonalScopeDoesNotRequireTeamAdmin(t *testing.T) {
	req := credentialAccessRequest("/api/credentials/team-secret", "personal")
	svc := &store.Services{Mode: store.ModePlatform, PersonalCredentials: &mockCredentialStore{}}
	req = req.WithContext(store.WithServices(req.Context(), svc))

	w := httptest.NewRecorder()
	GetCredentialHandler(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("explicit personal scope was incorrectly treated as team access: %s", w.Body.String())
	}
}
