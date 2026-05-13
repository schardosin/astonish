package filestore

import (
	"context"

	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/store"
)

// CredentialStoreWrapper wraps the existing credentials.Store behind the
// store.CredentialStore interface.
type CredentialStoreWrapper struct {
	inner *credentials.Store
}

// NewCredentialStore creates a CredentialStore backed by the existing encrypted file store.
func NewCredentialStore(cs *credentials.Store) store.CredentialStore {
	return &CredentialStoreWrapper{inner: cs}
}

// Inner returns the underlying credentials.Store for code that still needs
// direct access during the transition period.
func (w *CredentialStoreWrapper) Inner() *credentials.Store {
	return w.inner
}

func (w *CredentialStoreWrapper) Get(_ context.Context, name string) *store.Credential {
	c := w.inner.Get(name)
	if c == nil {
		return nil
	}
	return convertCredential(c)
}

func (w *CredentialStoreWrapper) Set(_ context.Context, name string, cred *store.Credential) error {
	return w.inner.Set(name, convertToInternalCred(cred))
}

func (w *CredentialStoreWrapper) Remove(_ context.Context, name string) error {
	return w.inner.Remove(name)
}

func (w *CredentialStoreWrapper) List(_ context.Context) map[string]store.CredentialType {
	internal := w.inner.List()
	result := make(map[string]store.CredentialType, len(internal))
	for k, v := range internal {
		result[k] = store.CredentialType(v)
	}
	return result
}

func (w *CredentialStoreWrapper) Count(_ context.Context) int {
	return w.inner.Count()
}

func (w *CredentialStoreWrapper) Resolve(_ context.Context, name string) (headerKey, headerValue string, err error) {
	return w.inner.Resolve(name)
}

func (w *CredentialStoreWrapper) SetSecret(_ context.Context, key, value string) error {
	return w.inner.SetSecret(key, value)
}

func (w *CredentialStoreWrapper) SetSecretBatch(_ context.Context, secrets map[string]string) error {
	return w.inner.SetSecretBatch(secrets)
}

func (w *CredentialStoreWrapper) GetSecret(_ context.Context, key string) string {
	return w.inner.GetSecret(key)
}

func (w *CredentialStoreWrapper) RemoveSecret(_ context.Context, key string) error {
	return w.inner.RemoveSecret(key)
}

func (w *CredentialStoreWrapper) HasSecrets(_ context.Context) bool {
	return w.inner.HasSecrets()
}

func (w *CredentialStoreWrapper) SecretCount(_ context.Context) int {
	return w.inner.SecretCount()
}

func (w *CredentialStoreWrapper) ListSecrets(_ context.Context) []string {
	return w.inner.ListSecrets()
}

func (w *CredentialStoreWrapper) Reload(_ context.Context) error {
	return w.inner.Reload()
}

func convertCredential(c *credentials.Credential) *store.Credential {
	return &store.Credential{
		Type:         store.CredentialType(c.Type),
		Header:       c.Header,
		Value:        c.Value,
		Token:        c.Token,
		Username:     c.Username,
		Password:     c.Password,
		AuthURL:      c.AuthURL,
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Scope:        c.Scope,
		TokenURL:     c.TokenURL,
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		TokenExpiry:  c.TokenExpiry,
	}
}

func convertToInternalCred(c *store.Credential) *credentials.Credential {
	return &credentials.Credential{
		Type:         credentials.CredentialType(c.Type),
		Header:       c.Header,
		Value:        c.Value,
		Token:        c.Token,
		Username:     c.Username,
		Password:     c.Password,
		AuthURL:      c.AuthURL,
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Scope:        c.Scope,
		TokenURL:     c.TokenURL,
		AccessToken:  c.AccessToken,
		RefreshToken: c.RefreshToken,
		TokenExpiry:  c.TokenExpiry,
	}
}

// Compile-time check.
var _ store.CredentialStore = (*CredentialStoreWrapper)(nil)
