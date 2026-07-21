package backendpool_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/SAP/astonish/pkg/sandbox"
	"github.com/SAP/astonish/pkg/sandbox/mock"
	"github.com/SAP/astonish/pkg/sandbox/sessioncreds"
	"github.com/SAP/astonish/pkg/store"
)

type vaultMemStore struct {
	creds map[string]*store.Credential
}

func (m *vaultMemStore) Get(_ context.Context, name string) *store.Credential { return m.creds[name] }
func (m *vaultMemStore) Set(_ context.Context, name string, cred *store.Credential) error {
	m.creds[name] = cred
	return nil
}
func (m *vaultMemStore) Remove(_ context.Context, name string) error {
	delete(m.creds, name)
	return nil
}
func (m *vaultMemStore) List(_ context.Context) map[string]store.CredentialType {
	out := make(map[string]store.CredentialType, len(m.creds))
	for k, v := range m.creds {
		out[k] = v.Type
	}
	return out
}
func (m *vaultMemStore) Count(_ context.Context) int { return len(m.creds) }
func (m *vaultMemStore) Resolve(_ context.Context, name string) (string, string, error) {
	return store.ResolveCredentialHeader(name, m.Get(context.Background(), name), nil)
}
func (m *vaultMemStore) InvalidateToken(context.Context, string)              {}
func (m *vaultMemStore) SetSecret(context.Context, string, string) error      { return nil }
func (m *vaultMemStore) SetSecretBatch(context.Context, map[string]string) error {
	return nil
}
func (m *vaultMemStore) GetSecret(context.Context, string) string   { return "" }
func (m *vaultMemStore) RemoveSecret(context.Context, string) error { return nil }
func (m *vaultMemStore) HasSecrets(context.Context) bool            { return false }
func (m *vaultMemStore) SecretCount(context.Context) int            { return 0 }
func (m *vaultMemStore) ListSecrets(context.Context) []string       { return nil }
func (m *vaultMemStore) Reload(context.Context) error               { return nil }

func TestSyncSessionCredentialVault_PushesFile(t *testing.T) {
	m := mock.New()
	_, err := m.CreateSession(context.Background(), sandbox.SessionSpec{SessionID: "sess-1"})
	if err != nil {
		t.Fatal(err)
	}

	cs := &vaultMemStore{creds: map[string]*store.Credential{
		"api": {Type: store.CredBearer, Token: "tok-xyz"},
	}}
	ctx := store.WithCredentialStore(context.Background(), cs)

	readyCalled := false
	err = sandbox.SyncSessionCredentialVault(ctx, m, func(sessionID string) error {
		readyCalled = true
		if sessionID != "sess-1" {
			t.Fatalf("sessionID = %q", sessionID)
		}
		return nil
	}, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if !readyCalled {
		t.Fatal("expected EnsureReady callback")
	}

	calls := m.PushFileCalls()
	if len(calls) != 1 {
		t.Fatalf("PushFile calls = %d, want 1", len(calls))
	}
	if calls[0].Path != sessioncreds.VaultPath {
		t.Fatalf("path = %q, want %q", calls[0].Path, sessioncreds.VaultPath)
	}
	if !bytes.Contains(calls[0].Content, []byte("tok-xyz")) {
		t.Fatalf("vault missing token: %s", calls[0].Content)
	}
}

func TestSyncSessionCredentialVault_NoopWithoutStore(t *testing.T) {
	m := mock.New()
	err := sandbox.SyncSessionCredentialVault(context.Background(), m, nil, "sess-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.PushFileCalls()) != 0 {
		t.Fatal("expected no PushFile without credential store")
	}
}
