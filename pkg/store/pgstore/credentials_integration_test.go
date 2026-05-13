//go:build integration

package pgstore

import (
	"context"
	"crypto/rand"
	"testing"

	"github.com/schardosin/astonish/pkg/store"
)

// testEncKey generates a random 32-byte AES-256 key for testing.
func testEncKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("failed to generate test encryption key: %v", err)
	}
	return key
}

func TestCredentialStore_CRUD(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()
	encKey := testEncKey(t)

	cs := &pgCredentialStore{pool: pool, schema: schema, encKey: encKey}

	// Set a credential
	cred := &store.Credential{
		Type:  store.CredBearer,
		Value: "my-bearer-token-123",
	}
	if err := cs.Set(ctx, "github-token", cred); err != nil {
		t.Fatalf("Set() failed: %v", err)
	}

	// Get it back
	got := cs.Get(ctx, "github-token")
	if got == nil {
		t.Fatal("Get() returned nil after Set()")
	}
	if got.Type != store.CredBearer {
		t.Errorf("Type = %q, want %q", got.Type, store.CredBearer)
	}
	if got.Value != "my-bearer-token-123" {
		t.Errorf("Value = %q, want %q", got.Value, "my-bearer-token-123")
	}

	// Remove it
	if err := cs.Remove(ctx, "github-token"); err != nil {
		t.Fatalf("Remove() failed: %v", err)
	}

	// Verify gone
	if got := cs.Get(ctx, "github-token"); got != nil {
		t.Errorf("Get() after Remove should return nil, got %+v", got)
	}
}

func TestCredentialStore_SecretsCRUD(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()
	encKey := testEncKey(t)

	cs := &pgCredentialStore{pool: pool, schema: schema, encKey: encKey}

	// SetSecret
	if err := cs.SetSecret(ctx, "OPENAI_API_KEY", "sk-test-key-12345"); err != nil {
		t.Fatalf("SetSecret() failed: %v", err)
	}

	// GetSecret
	val := cs.GetSecret(ctx, "OPENAI_API_KEY")
	if val != "sk-test-key-12345" {
		t.Errorf("GetSecret() = %q, want %q", val, "sk-test-key-12345")
	}

	// HasSecrets
	if !cs.HasSecrets(ctx) {
		t.Error("HasSecrets() = false, want true")
	}

	// SecretCount
	if count := cs.SecretCount(ctx); count != 1 {
		t.Errorf("SecretCount() = %d, want 1", count)
	}

	// ListSecrets
	secrets := cs.ListSecrets(ctx)
	if len(secrets) != 1 || secrets[0] != "OPENAI_API_KEY" {
		t.Errorf("ListSecrets() = %v, want [OPENAI_API_KEY]", secrets)
	}

	// RemoveSecret
	if err := cs.RemoveSecret(ctx, "OPENAI_API_KEY"); err != nil {
		t.Fatalf("RemoveSecret() failed: %v", err)
	}

	// Verify gone
	if val := cs.GetSecret(ctx, "OPENAI_API_KEY"); val != "" {
		t.Errorf("GetSecret() after RemoveSecret = %q, want empty", val)
	}
	if cs.HasSecrets(ctx) {
		t.Error("HasSecrets() after RemoveSecret = true, want false")
	}
}

func TestCredentialStore_ListAndCount(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()
	encKey := testEncKey(t)

	cs := &pgCredentialStore{pool: pool, schema: schema, encKey: encKey}

	// Initial state: empty
	if count := cs.Count(ctx); count != 0 {
		t.Errorf("initial Count() = %d, want 0", count)
	}
	if list := cs.List(ctx); len(list) != 0 {
		t.Errorf("initial List() has %d entries, want 0", len(list))
	}

	// Add multiple credentials
	creds := map[string]*store.Credential{
		"api-key-1": {Type: store.CredAPIKey, Value: "key1"},
		"api-key-2": {Type: store.CredAPIKey, Value: "key2"},
		"bearer-1":  {Type: store.CredBearer, Value: "tok1"},
	}
	for name, cred := range creds {
		if err := cs.Set(ctx, name, cred); err != nil {
			t.Fatalf("Set(%s) failed: %v", name, err)
		}
	}

	// Count should be 3
	if count := cs.Count(ctx); count != 3 {
		t.Errorf("Count() = %d, want 3", count)
	}

	// List should have 3 entries with correct types
	list := cs.List(ctx)
	if len(list) != 3 {
		t.Fatalf("List() has %d entries, want 3", len(list))
	}
	if list["api-key-1"] != store.CredAPIKey {
		t.Errorf("List()[api-key-1] = %q, want %q", list["api-key-1"], store.CredAPIKey)
	}
	if list["bearer-1"] != store.CredBearer {
		t.Errorf("List()[bearer-1] = %q, want %q", list["bearer-1"], store.CredBearer)
	}

	// Secrets should NOT appear in List/Count
	if err := cs.SetSecret(ctx, "MY_SECRET", "hidden"); err != nil {
		t.Fatalf("SetSecret() failed: %v", err)
	}
	if count := cs.Count(ctx); count != 3 {
		t.Errorf("Count() after adding secret = %d, want 3 (secrets excluded)", count)
	}
	if list := cs.List(ctx); len(list) != 3 {
		t.Errorf("List() after adding secret has %d entries, want 3 (secrets excluded)", len(list))
	}
}

func TestCredentialStore_Resolve(t *testing.T) {
	pool := testPool(t)
	schema := setupTestSchema(t, pool)
	ctx := context.Background()
	encKey := testEncKey(t)

	cs := &pgCredentialStore{pool: pool, schema: schema, encKey: encKey}

	// Set a bearer credential
	cred := &store.Credential{
		Type:  store.CredBearer,
		Token: "my-bearer-resolve-test",
	}
	if err := cs.Set(ctx, "test-bearer", cred); err != nil {
		t.Fatalf("Set() failed: %v", err)
	}

	// Resolve should return Authorization header
	headerKey, headerValue, err := cs.Resolve(ctx, "test-bearer")
	if err != nil {
		t.Fatalf("Resolve() failed: %v", err)
	}
	if headerKey != "Authorization" {
		t.Errorf("Resolve() headerKey = %q, want %q", headerKey, "Authorization")
	}
	if headerValue != "Bearer my-bearer-resolve-test" {
		t.Errorf("Resolve() headerValue = %q, want %q", headerValue, "Bearer my-bearer-resolve-test")
	}

	// Resolve non-existent credential should error
	_, _, err = cs.Resolve(ctx, "does-not-exist")
	if err == nil {
		t.Error("Resolve() for non-existent credential should return error")
	}
}
