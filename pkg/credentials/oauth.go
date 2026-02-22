package credentials

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	// tokenExpiryBuffer is subtracted from the token's actual expiry time
	// to ensure we refresh before it becomes invalid.
	tokenExpiryBuffer = 30 * time.Second

	// oauthTimeout is the HTTP timeout for OAuth token requests.
	oauthTimeout = 15 * time.Second

	// maxTokenResponseBytes is the maximum size of an OAuth token response.
	maxTokenResponseBytes = 64 * 1024
)

// cachedToken holds an in-memory OAuth access token with its expiry time.
type cachedToken struct {
	accessToken string
	expiresAt   time.Time
}

// tokenCache manages cached OAuth access tokens keyed by credential name.
type tokenCache struct {
	mu     sync.RWMutex
	tokens map[string]*cachedToken
}

func newTokenCache() *tokenCache {
	return &tokenCache{
		tokens: make(map[string]*cachedToken),
	}
}

// GetOrRefresh returns a valid access token for the named OAuth credential.
// If the cached token is still valid, it is returned immediately.
// Otherwise, a new token is acquired via the client_credentials flow.
func (tc *tokenCache) GetOrRefresh(name string, cred *Credential, redactor *Redactor) (string, error) {
	tc.mu.RLock()
	if cached, ok := tc.tokens[name]; ok {
		if time.Now().Before(cached.expiresAt) {
			tc.mu.RUnlock()
			return cached.accessToken, nil
		}
	}
	tc.mu.RUnlock()

	// Token expired or not cached — acquire a new one
	token, expiresIn, err := fetchOAuthToken(cred)
	if err != nil {
		return "", err
	}

	expiresAt := time.Now().Add(time.Duration(expiresIn)*time.Second - tokenExpiryBuffer)
	if expiresAt.Before(time.Now()) {
		// Token has a very short lifetime, cache for at least 10 seconds
		expiresAt = time.Now().Add(10 * time.Second)
	}

	tc.mu.Lock()
	tc.tokens[name] = &cachedToken{
		accessToken: token,
		expiresAt:   expiresAt,
	}
	tc.mu.Unlock()

	// Register the token for redaction so it can't leak through tool outputs
	if redactor != nil {
		redactor.AddSecret(name+"/token", token)
	}

	return token, nil
}

// oauthTokenResponse is the JSON response from an OAuth token endpoint.
type oauthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Error       string `json:"error"`
	ErrorDesc   string `json:"error_description"`
}

// fetchOAuthToken performs the OAuth2 client_credentials flow.
func fetchOAuthToken(cred *Credential) (token string, expiresIn int, err error) {
	if cred.AuthURL == "" {
		return "", 0, fmt.Errorf("auth_url is required for OAuth credentials")
	}
	if cred.ClientID == "" {
		return "", 0, fmt.Errorf("client_id is required for OAuth credentials")
	}
	if cred.ClientSecret == "" {
		return "", 0, fmt.Errorf("client_secret is required for OAuth credentials")
	}

	// Build form-encoded body
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {cred.ClientID},
		"client_secret": {cred.ClientSecret},
	}
	if cred.Scope != "" {
		form.Set("scope", cred.Scope)
	}

	client := &http.Client{Timeout: oauthTimeout}
	resp, err := client.Post(cred.AuthURL, "application/x-www-form-urlencoded", strings.NewReader(form.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return "", 0, fmt.Errorf("read token response: %w", err)
	}

	var tokenResp oauthTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return "", 0, fmt.Errorf("parse token response: %w", err)
	}

	if tokenResp.Error != "" {
		return "", 0, fmt.Errorf("OAuth error: %s (%s)", tokenResp.Error, tokenResp.ErrorDesc)
	}

	if tokenResp.AccessToken == "" {
		return "", 0, fmt.Errorf("token response missing access_token (status %d)", resp.StatusCode)
	}

	expiresIn = tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // Default 1 hour if not specified
	}

	return tokenResp.AccessToken, expiresIn, nil
}
