package credentials

import "github.com/schardosin/astonish/pkg/store"

// StoreAdapter wraps a store.CredentialStore to satisfy CredentialResolver.
// It converts *store.Credential → *credentials.Credential so the substitution
// engine works identically for both file-based and PG-backed credential stores.
type StoreAdapter struct {
	inner store.CredentialStore
}

// NewStoreAdapter wraps a store.CredentialStore as a CredentialResolver.
func NewStoreAdapter(cs store.CredentialStore) CredentialResolver {
	if cs == nil {
		return nil
	}
	return &StoreAdapter{inner: cs}
}

func (a *StoreAdapter) Get(name string) *Credential {
	sc := a.inner.Get(name)
	if sc == nil {
		return nil
	}
	return &Credential{
		Type:         CredentialType(sc.Type),
		Header:       sc.Header,
		Value:        sc.Value,
		Token:        sc.Token,
		Username:     sc.Username,
		Password:     sc.Password,
		AuthURL:      sc.AuthURL,
		ClientID:     sc.ClientID,
		ClientSecret: sc.ClientSecret,
		Scope:        sc.Scope,
		TokenURL:     sc.TokenURL,
		AccessToken:  sc.AccessToken,
		RefreshToken: sc.RefreshToken,
		TokenExpiry:  sc.TokenExpiry,
	}
}

func (a *StoreAdapter) Resolve(name string) (headerKey, headerValue string, err error) {
	return a.inner.Resolve(name)
}

func (a *StoreAdapter) Reload() error {
	return a.inner.Reload()
}

// Compile-time check.
var _ CredentialResolver = (*StoreAdapter)(nil)
