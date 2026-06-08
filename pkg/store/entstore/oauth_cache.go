package entstore

import (
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/store"
)

const (
	// oauthExpiryBuffer is subtracted from the token's actual expiry time
	// to ensure we refresh before it becomes invalid.
	oauthExpiryBuffer = 30 * time.Second
)

// oauthCachedToken holds an in-memory OAuth access token with its expiry time.
type oauthCachedToken struct {
	accessToken string
	expiresAt   time.Time
}

// oauthTokenCache manages cached OAuth access tokens keyed by credential name.
// It is shared across all DB-backed credential stores in the same process.
type oauthTokenCache struct {
	mu     sync.RWMutex
	tokens map[string]*oauthCachedToken
}

// globalOAuthCache is a process-level cache shared by all entstore credential stores.
var globalOAuthCache = &oauthTokenCache{
	tokens: make(map[string]*oauthCachedToken),
}

// getOrFetch returns a cached token if still valid, otherwise fetches a new one
// via the OAuth2 client_credentials flow.
func (tc *oauthTokenCache) getOrFetch(name string, cred *store.Credential) (string, error) {
	tc.mu.RLock()
	if cached, ok := tc.tokens[name]; ok {
		if time.Now().Before(cached.expiresAt) {
			tc.mu.RUnlock()
			return cached.accessToken, nil
		}
	}
	tc.mu.RUnlock()

	// Token expired or not cached — acquire a new one.
	internalCred := &credentials.Credential{
		Type:         credentials.CredentialType(cred.Type),
		AuthURL:      cred.AuthURL,
		ClientID:     cred.ClientID,
		ClientSecret: cred.ClientSecret,
		Scope:        cred.Scope,
	}

	token, expiresIn, err := credentials.FetchOAuthToken(internalCred)
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(time.Duration(expiresIn)*time.Second - oauthExpiryBuffer)
	if expiresAt.Before(time.Now()) {
		// Token has a very short lifetime, cache for at least 10 seconds.
		expiresAt = time.Now().Add(10 * time.Second)
	}

	tc.mu.Lock()
	tc.tokens[name] = &oauthCachedToken{
		accessToken: token,
		expiresAt:   expiresAt,
	}
	tc.mu.Unlock()

	return token, nil
}

// oauthFetcher returns an OAuthTokenFetcher function bound to the given
// credential name for token caching purposes.
func oauthFetcher(name string) store.OAuthTokenFetcher {
	return func(cred *store.Credential) (string, error) {
		return globalOAuthCache.getOrFetch(name, cred)
	}
}
