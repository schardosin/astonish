package credentials

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchKeystoneToken_ApplicationCredential(t *testing.T) {
	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("X-Subject-Token", "gAAAA-keystone-app-cred-token")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": map[string]any{
				"expires_at": expiresAt.Format(time.RFC3339),
			},
		})
	}))
	defer srv.Close()

	cred := &Credential{
		Type:                        CredOpenStackKeystone,
		AuthURL:                     srv.URL,
		ApplicationCredentialID:     "app-cred-id-123",
		ApplicationCredentialSecret: "app-cred-secret-xyz",
	}

	token, gotExpiry, err := FetchKeystoneToken(cred)
	if err != nil {
		t.Fatalf("FetchKeystoneToken: %v", err)
	}
	if token != "gAAAA-keystone-app-cred-token" {
		t.Errorf("token = %q", token)
	}
	if !gotExpiry.Equal(expiresAt) {
		t.Errorf("expiresAt = %v, want %v", gotExpiry, expiresAt)
	}

	auth := gotBody["auth"].(map[string]any)
	identity := auth["identity"].(map[string]any)
	methods := identity["methods"].([]any)
	if len(methods) != 1 || methods[0] != "application_credential" {
		t.Errorf("methods = %v", methods)
	}
	appCred := identity["application_credential"].(map[string]any)
	if appCred["id"] != "app-cred-id-123" || appCred["secret"] != "app-cred-secret-xyz" {
		t.Errorf("application_credential = %v", appCred)
	}
	if _, hasScope := auth["scope"]; hasScope {
		t.Error("application credential auth should not include scope")
	}
}

func TestFetchKeystoneToken_Password(t *testing.T) {
	expiresAt := time.Now().UTC().Add(2 * time.Hour).Truncate(time.Second)
	var gotBody map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("X-Subject-Token", "gAAAA-keystone-password-token")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": map[string]any{
				"expires_at": expiresAt.Format(time.RFC3339),
			},
		})
	}))
	defer srv.Close()

	cred := &Credential{
		Type:          CredOpenStackKeystone,
		AuthURL:       srv.URL,
		Username:      "demo",
		Password:      "secretpass",
		UserDomain:    "Default",
		ProjectName:   "my-project",
		ProjectDomain: "Default",
	}

	token, _, err := FetchKeystoneToken(cred)
	if err != nil {
		t.Fatalf("FetchKeystoneToken: %v", err)
	}
	if token != "gAAAA-keystone-password-token" {
		t.Errorf("token = %q", token)
	}

	auth := gotBody["auth"].(map[string]any)
	identity := auth["identity"].(map[string]any)
	methods := identity["methods"].([]any)
	if methods[0] != "password" {
		t.Errorf("methods = %v", methods)
	}
	user := identity["password"].(map[string]any)["user"].(map[string]any)
	if user["name"] != "demo" || user["password"] != "secretpass" {
		t.Errorf("user = %v", user)
	}
	scope := auth["scope"].(map[string]any)
	project := scope["project"].(map[string]any)
	if project["name"] != "my-project" {
		t.Errorf("project = %v", project)
	}
}

func TestFetchKeystoneToken_PasswordProjectID(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("X-Subject-Token", "tok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"token":{"expires_at":"2099-01-01T00:00:00Z"}}`))
	}))
	defer srv.Close()

	cred := &Credential{
		Type:      CredOpenStackKeystone,
		AuthURL:   srv.URL,
		Username:  "u",
		Password:  "p",
		ProjectID: "proj-uuid-123",
	}
	if _, _, err := FetchKeystoneToken(cred); err != nil {
		t.Fatalf("FetchKeystoneToken: %v", err)
	}
	project := gotBody["auth"].(map[string]any)["scope"].(map[string]any)["project"].(map[string]any)
	if project["id"] != "proj-uuid-123" {
		t.Errorf("project = %v", project)
	}
	if _, hasDomain := project["domain"]; hasDomain {
		t.Error("project_id scope should not include domain")
	}
}

func TestFetchKeystoneToken_MissingTokenHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401,"title":"Unauthorized","message":"The request you have made requires authentication."}}`))
	}))
	defer srv.Close()

	cred := &Credential{
		Type:                        CredOpenStackKeystone,
		AuthURL:                     srv.URL,
		ApplicationCredentialID:     "id",
		ApplicationCredentialSecret: "secret",
	}
	_, _, err := FetchKeystoneToken(cred)
	if err == nil {
		t.Fatal("expected error")
	}
	if !contains(err.Error(), "Keystone error") && !contains(err.Error(), "X-Subject-Token") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFetchKeystoneToken_Validation(t *testing.T) {
	tests := []struct {
		name string
		cred *Credential
	}{
		{"no auth_url", &Credential{Type: CredOpenStackKeystone, Username: "u", Password: "p", ProjectID: "p"}},
		{"no method", &Credential{Type: CredOpenStackKeystone, AuthURL: "http://x"}},
		{"password no project", &Credential{Type: CredOpenStackKeystone, AuthURL: "http://x", Username: "u", Password: "p"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := FetchKeystoneToken(tt.cred)
			if err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestKeystoneTokenCache(t *testing.T) {
	tc := newTokenCache()
	r := NewRedactor()

	tc.mu.Lock()
	tc.tokens["ks"] = &cachedToken{
		accessToken: "cached-keystone-token",
		expiresAt:   time.Now().Add(300 * time.Second),
	}
	tc.mu.Unlock()

	cred := &Credential{
		Type:                        CredOpenStackKeystone,
		AuthURL:                     "https://identity.example.com/v3/auth/tokens",
		ApplicationCredentialID:     "id",
		ApplicationCredentialSecret: "secret",
	}

	token, err := tc.GetOrRefreshKeystone("ks", cred, r)
	if err != nil {
		t.Fatalf("GetOrRefreshKeystone: %v", err)
	}
	if token != "cached-keystone-token" {
		t.Errorf("token = %q", token)
	}
}

func TestStoreResolve_Keystone(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	expiresAt := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Subject-Token", "resolved-keystone-token-abc")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token": map[string]any{"expires_at": expiresAt.Format(time.RFC3339)},
		})
	}))
	defer srv.Close()

	if err := s.Set("openstack", &Credential{
		Type:                        CredOpenStackKeystone,
		AuthURL:                     srv.URL,
		ApplicationCredentialID:     "app-id",
		ApplicationCredentialSecret: "app-secret-long-enough",
	}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	headerKey, headerValue, err := s.Resolve("openstack")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if headerKey != "X-Auth-Token" {
		t.Errorf("headerKey = %q, want X-Auth-Token", headerKey)
	}
	if headerValue != "resolved-keystone-token-abc" {
		t.Errorf("headerValue = %q", headerValue)
	}

	// Second resolve should hit cache (server can be closed)
	srv.Close()
	headerKey2, headerValue2, err := s.Resolve("openstack")
	if err != nil {
		t.Fatalf("Resolve (cached): %v", err)
	}
	if headerKey2 != "X-Auth-Token" || headerValue2 != "resolved-keystone-token-abc" {
		t.Errorf("cached resolve = %s: %s", headerKey2, headerValue2)
	}

	// Invalidate and ensure next resolve fails (server closed)
	s.InvalidateToken("openstack")
	_, _, err = s.Resolve("openstack")
	if err == nil {
		t.Fatal("expected error after invalidate with closed server")
	}
}

func TestBuildKeystoneAuthBody_AppCredTakesPrecedence(t *testing.T) {
	cred := &Credential{
		Type:                        CredOpenStackKeystone,
		AuthURL:                     "https://identity.example.com/v3/auth/tokens",
		Username:                    "user",
		Password:                    "pass",
		ProjectID:                   "proj",
		ApplicationCredentialID:     "app-id",
		ApplicationCredentialSecret: "app-secret",
	}
	body, err := buildKeystoneAuthBody(cred)
	if err != nil {
		t.Fatalf("buildKeystoneAuthBody: %v", err)
	}
	var parsed keystoneAuthRequest
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if parsed.Auth.Identity.Methods[0] != "application_credential" {
		t.Errorf("expected application_credential method, got %v", parsed.Auth.Identity.Methods)
	}
	if parsed.Auth.Scope != nil {
		t.Error("app cred should not set scope")
	}
}
