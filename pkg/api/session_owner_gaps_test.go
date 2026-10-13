package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/gorilla/mux"
)

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
	req := withSandboxRegistry(httptest.NewRequest(http.MethodGet, "/", nil), sandbox.NewSessionRegistryFromStore(registry))
	w := httptest.NewRecorder()

	if artifactSessionVisible(w, req, "foreign-session") {
		t.Fatal("artifact access without a sandbox must still require an owned session")
	}
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}
