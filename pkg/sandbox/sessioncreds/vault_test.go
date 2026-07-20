package sessioncreds

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/SAP/astonish/pkg/store"
)

type memStore struct {
	creds map[string]*store.Credential
}

func (m *memStore) Get(_ context.Context, name string) *store.Credential { return m.creds[name] }
func (m *memStore) Set(_ context.Context, name string, cred *store.Credential) error {
	m.creds[name] = cred
	return nil
}
func (m *memStore) Remove(_ context.Context, name string) error {
	delete(m.creds, name)
	return nil
}
func (m *memStore) List(_ context.Context) map[string]store.CredentialType {
	out := make(map[string]store.CredentialType, len(m.creds))
	for k, v := range m.creds {
		out[k] = v.Type
	}
	return out
}
func (m *memStore) Count(_ context.Context) int { return len(m.creds) }
func (m *memStore) Resolve(_ context.Context, name string) (string, string, error) {
	return store.ResolveCredentialHeader(name, m.Get(context.Background(), name), nil)
}
func (m *memStore) InvalidateToken(context.Context, string)              {}
func (m *memStore) SetSecret(context.Context, string, string) error      { return nil }
func (m *memStore) SetSecretBatch(context.Context, map[string]string) error {
	return nil
}
func (m *memStore) GetSecret(context.Context, string) string   { return "" }
func (m *memStore) RemoveSecret(context.Context, string) error { return nil }
func (m *memStore) HasSecrets(context.Context) bool            { return false }
func (m *memStore) SecretCount(context.Context) int            { return 0 }
func (m *memStore) ListSecrets(context.Context) []string       { return nil }
func (m *memStore) Reload(context.Context) error               { return nil }

func TestSerializeRoundTrip_Bearer(t *testing.T) {
	cs := &memStore{creds: map[string]*store.Credential{
		"api": {Type: store.CredBearer, Token: "tok-123"},
		"_secret:x": {Type: "_secret", Value: "should-skip"},
	}}
	data, err := Serialize(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	var vf vaultFile
	if err := json.Unmarshal(data, &vf); err != nil {
		t.Fatal(err)
	}
	if _, ok := vf.Credentials["_secret:x"]; ok {
		t.Fatal("secrets must not be serialized")
	}
	loaded, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	key, val, err := loaded.Resolve(context.Background(), "api")
	if err != nil {
		t.Fatal(err)
	}
	if key != "Authorization" || val != "Bearer tok-123" {
		t.Fatalf("got %s=%s", key, val)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Count(context.Background()) != 0 {
		t.Fatalf("want empty store, got %d", s.Count(context.Background()))
	}
}

func TestLoad_FromDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault.json")
	cs := &memStore{creds: map[string]*store.Credential{
		"k": {Type: store.CredAPIKey, Header: "X-Key", Value: "v"},
	}}
	data, err := Serialize(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	key, val, err := loaded.Resolve(context.Background(), "k")
	if err != nil {
		t.Fatal(err)
	}
	if key != "X-Key" || val != "v" {
		t.Fatalf("got %s=%s", key, val)
	}
}

func TestResolve_KeystoneFetchesInProcess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Subject-Token", "ks-token")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":{"expires_at":"2099-01-01T00:00:00.000000Z"}}`))
	}))
	defer srv.Close()

	s := NewStore(map[string]*store.Credential{
		"openstack": {
			Type:                        store.CredOpenStackKeystone,
			AuthURL:                     srv.URL,
			ApplicationCredentialID:     "app-id",
			ApplicationCredentialSecret: "app-secret",
		},
	})
	key, val, err := s.Resolve(context.Background(), "openstack")
	if err != nil {
		t.Fatal(err)
	}
	if key != "X-Auth-Token" || val != "ks-token" {
		t.Fatalf("got %s=%s", key, val)
	}
}
