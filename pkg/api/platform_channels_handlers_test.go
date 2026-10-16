package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SAP/astonish/pkg/store"
	"github.com/gorilla/mux"
)

type emailSettingsFixture struct{ settings *store.PlatformSettings }

func (s *emailSettingsFixture) Get(context.Context) (*store.PlatformSettings, error) {
	return s.settings, nil
}
func (s *emailSettingsFixture) Save(_ context.Context, v *store.PlatformSettings) error {
	s.settings = v
	return nil
}

type emailSettingsBackend struct {
	mockPlatformBackendForChannelInfo
	settings *emailSettingsFixture
}

func (b *emailSettingsBackend) PlatformSettings() store.PlatformSettingsStore { return b.settings }

type emailSettingsSecrets struct{}

func (emailSettingsSecrets) GetSecret(string) string        { return "" }
func (emailSettingsSecrets) SetSecret(string, string) error { return nil }
func (emailSettingsSecrets) RemoveSecret(string) error      { return nil }

func TestPlatformEmailAuthenticationSettingsRoundTrip(t *testing.T) {
	oldBackend, oldSecrets := platformBackendInstance, platformSecretsInstance
	defer func() { platformBackendInstance, platformSecretsInstance = oldBackend, oldSecrets }()
	fixture := &emailSettingsFixture{settings: &store.PlatformSettings{}}
	platformBackendInstance = &emailSettingsBackend{settings: fixture}
	platformSecretsInstance = emailSettingsSecrets{}
	req := httptest.NewRequest(http.MethodPut, "/api/platform/admin/channels/email", strings.NewReader(`{"enabled":true,"config":{"provider":"msgraph","address":"bot@example.com","trusted_authserv_id":"receiver.example","sender_auth_mode":"strict"}}`))
	req = mux.SetURLVars(req, map[string]string{"type": "email"})
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{PlatformRole: "superadmin"}))
	w := httptest.NewRecorder()
	PlatformAdminSaveChannelHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body.String())
	}
	saved := fixture.settings.Channels.Email
	if saved.TrustedAuthservID != "receiver.example" || saved.SenderAuthMode != "strict" || saved.RequireSenderAuth != nil {
		t.Fatalf("lost authentication settings: %+v", saved)
	}

	// Older clients omit the auth fields; saving unrelated settings must keep
	// the trusted receiver identity and the fail-closed sender-auth setting.
	req = httptest.NewRequest(http.MethodPut, "/api/platform/admin/channels/email", strings.NewReader(`{"enabled":true,"config":{"provider":"msgraph","address":"bot@example.com"}}`))
	req = mux.SetURLVars(req, map[string]string{"type": "email"})
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{PlatformRole: "superadmin"}))
	w = httptest.NewRecorder()
	PlatformAdminSaveChannelHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("legacy save status=%d body=%s", w.Code, w.Body.String())
	}
	saved = fixture.settings.Channels.Email
	if saved.TrustedAuthservID != "receiver.example" || saved.SenderAuthMode != "strict" {
		t.Fatalf("legacy save lost authentication settings: %+v", saved)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/platform/admin/channels", nil)
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{PlatformRole: "superadmin"}))
	w = httptest.NewRecorder()
	PlatformAdminListChannelsHandler(w, req)
	var result []struct {
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, channel := range result {
		if channel.Type == "email" {
			if channel.Config["trusted_authserv_id"] != "receiver.example" || channel.Config["sender_auth_mode"] != "strict" {
				t.Fatalf("missing round-trip fields: %+v", channel.Config)
			}
			return
		}
	}
	t.Fatal("email config missing")
}

func TestPlatformEmailProviderTrustMode(t *testing.T) {
	oldBackend, oldSecrets := platformBackendInstance, platformSecretsInstance
	defer func() { platformBackendInstance, platformSecretsInstance = oldBackend, oldSecrets }()
	fixture := &emailSettingsFixture{settings: &store.PlatformSettings{}}
	platformBackendInstance = &emailSettingsBackend{settings: fixture}
	platformSecretsInstance = emailSettingsSecrets{}

	req := httptest.NewRequest(http.MethodPut, "/api/platform/admin/channels/email", strings.NewReader(`{"enabled":true,"config":{"provider":"msgraph","address":"bot@example.com","sender_auth_mode":"provider-trust"}}`))
	req = mux.SetURLVars(req, map[string]string{"type": "email"})
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{PlatformRole: "superadmin"}))
	w := httptest.NewRecorder()
	PlatformAdminSaveChannelHandler(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("save status=%d body=%s", w.Code, w.Body.String())
	}
	saved := fixture.settings.Channels.Email
	if saved.SenderAuthMode != "provider-trust" || saved.RequireSenderAuth != nil {
		t.Fatalf("provider-trust was not persisted: %+v", saved)
	}

	req = httptest.NewRequest(http.MethodPut, "/api/platform/admin/channels/email", strings.NewReader(`{"enabled":true,"config":{"provider":"msgraph","address":"bot@example.com","sender_auth_mode":"maybe"}}`))
	req = mux.SetURLVars(req, map[string]string{"type": "email"})
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{PlatformRole: "superadmin"}))
	w = httptest.NewRecorder()
	PlatformAdminSaveChannelHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid mode status=%d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/platform/admin/channels", nil)
	req = req.WithContext(WithPlatformUser(req.Context(), &PlatformUser{PlatformRole: "superadmin"}))
	w = httptest.NewRecorder()
	PlatformAdminListChannelsHandler(w, req)
	var result []struct {
		Type   string         `json:"type"`
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, channel := range result {
		if channel.Type == "email" {
			if channel.Config["sender_auth_mode"] != "provider-trust" {
				t.Fatalf("list did not return provider-trust: %+v", channel.Config)
			}
			return
		}
	}
	t.Fatal("email config missing")
}
