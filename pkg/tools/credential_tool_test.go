package tools

import (
	"testing"

	"github.com/schardosin/astonish/pkg/credentials"
)

// setupCredentialStore creates a temp credential store and sets it as the global.
// Returns a cleanup function.
func setupCredentialStore(t *testing.T) func() {
	t.Helper()
	dir := t.TempDir()
	store, err := credentials.Open(dir)
	if err != nil {
		t.Fatalf("failed to open credential store: %v", err)
	}
	old := credentialStoreVar
	credentialStoreVar = store
	return func() { credentialStoreVar = old }
}

func TestSaveCredential_PasswordType(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	result, err := saveCredential(nil, SaveCredentialArgs{
		Name:     "proxmox-ssh",
		Type:     "password",
		Username: "root",
		Password: "my-secret-pass",
	})
	if err != nil {
		t.Fatalf("saveCredential: %v", err)
	}
	if result.Status != "saved" {
		t.Errorf("status = %q, want %q: %s", result.Status, "saved", result.Message)
	}

	// Verify it was stored
	cred := credentialStoreVar.Get("proxmox-ssh")
	if cred == nil {
		t.Fatal("credential not found after save")
	}
	if cred.Type != credentials.CredPassword {
		t.Errorf("type = %q, want %q", cred.Type, credentials.CredPassword)
	}
	if cred.Username != "root" {
		t.Errorf("username = %q, want %q", cred.Username, "root")
	}
	if cred.Password != "my-secret-pass" {
		t.Errorf("password = %q, want %q", cred.Password, "my-secret-pass")
	}
}

func TestSaveCredential_PasswordRequiresUsername(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	result, err := saveCredential(nil, SaveCredentialArgs{
		Name:     "no-user",
		Type:     "password",
		Password: "some-pass",
	})
	if err != nil {
		t.Fatalf("saveCredential: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("expected error status, got %q: %s", result.Status, result.Message)
	}
}

func TestResolveCredential_Password(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	// Save a password credential
	credentialStoreVar.Set("ssh-test", &credentials.Credential{
		Type:     credentials.CredPassword,
		Username: "admin",
		Password: "secret123",
	})

	result, err := resolveCredential(nil, ResolveCredentialArgs{Name: "ssh-test"})
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status = %q, want %q: %s", result.Status, "ok", result.Message)
	}
	if result.Type != "password" {
		t.Errorf("type = %q, want %q", result.Type, "password")
	}
	if result.Username != "admin" {
		t.Errorf("username = %q, want %q", result.Username, "admin")
	}
	if result.Password != "secret123" {
		t.Errorf("password = %q, want %q", result.Password, "secret123")
	}
}

func TestResolveCredential_Basic(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	credentialStoreVar.Set("http-basic", &credentials.Credential{
		Type:     credentials.CredBasic,
		Username: "user",
		Password: "pass",
	})

	result, err := resolveCredential(nil, ResolveCredentialArgs{Name: "http-basic"})
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status = %q, want %q", result.Status, "ok")
	}
	if result.Username != "user" || result.Password != "pass" {
		t.Errorf("fields = user=%q pass=%q, want user=user pass=pass", result.Username, result.Password)
	}
}

func TestResolveCredential_Bearer(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	credentialStoreVar.Set("my-bearer", &credentials.Credential{
		Type:  credentials.CredBearer,
		Token: "tok-123",
	})

	result, err := resolveCredential(nil, ResolveCredentialArgs{Name: "my-bearer"})
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if result.Token != "tok-123" {
		t.Errorf("token = %q, want %q", result.Token, "tok-123")
	}
}

func TestResolveCredential_APIKey(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	credentialStoreVar.Set("my-api", &credentials.Credential{
		Type:   credentials.CredAPIKey,
		Header: "X-Key",
		Value:  "sk-abc",
	})

	result, err := resolveCredential(nil, ResolveCredentialArgs{Name: "my-api"})
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if result.Header != "X-Key" || result.Value != "sk-abc" {
		t.Errorf("fields = header=%q value=%q, want X-Key/sk-abc", result.Header, result.Value)
	}
}

func TestResolveCredential_OAuth_NoSecret(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	credentialStoreVar.Set("my-oauth", &credentials.Credential{
		Type:         credentials.CredOAuthClientCreds,
		AuthURL:      "https://auth.example.com/token",
		ClientID:     "client-123",
		ClientSecret: "super-secret",
	})

	result, err := resolveCredential(nil, ResolveCredentialArgs{Name: "my-oauth"})
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if result.AuthURL != "https://auth.example.com/token" {
		t.Errorf("auth_url = %q", result.AuthURL)
	}
	if result.ClientID != "client-123" {
		t.Errorf("client_id = %q", result.ClientID)
	}
	// Client secret should NOT be returned
	if result.Password != "" || result.Token != "" || result.Value != "" {
		t.Error("OAuth resolve should not expose secret fields")
	}
}

func TestResolveCredential_NotFound(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	result, err := resolveCredential(nil, ResolveCredentialArgs{Name: "nonexistent"})
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("status = %q, want %q", result.Status, "error")
	}
}

func TestResolveCredential_NoStore(t *testing.T) {
	old := credentialStoreVar
	credentialStoreVar = nil
	defer func() { credentialStoreVar = old }()

	result, err := resolveCredential(nil, ResolveCredentialArgs{Name: "test"})
	if err != nil {
		t.Fatalf("resolveCredential: %v", err)
	}
	if result.Status != "error" {
		t.Errorf("status = %q, want %q", result.Status, "error")
	}
}

func TestTestCredential_PasswordType(t *testing.T) {
	cleanup := setupCredentialStore(t)
	defer cleanup()

	credentialStoreVar.Set("ssh-test", &credentials.Credential{
		Type:     credentials.CredPassword,
		Username: "root",
		Password: "pass",
	})

	result, err := testCredential(nil, TestCredentialArgs{Name: "ssh-test"})
	if err != nil {
		t.Fatalf("testCredential: %v", err)
	}
	if result.Status != "ok" {
		t.Errorf("status = %q, want %q: %s", result.Status, "ok", result.Message)
	}
}
