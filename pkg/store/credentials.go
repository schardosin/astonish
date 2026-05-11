package store

import (
	"encoding/base64"
	"fmt"
)

// CredentialType identifies the type of a stored credential.
type CredentialType = string

const (
	CredAPIKey           CredentialType = "api_key"
	CredBearer           CredentialType = "bearer"
	CredBasic            CredentialType = "basic"
	CredOAuthClientCreds CredentialType = "oauth_client_credentials"
	CredPassword         CredentialType = "password"
	CredOAuthAuthCode    CredentialType = "oauth_authorization_code"
)

// Credential represents a stored credential entry.
type Credential struct {
	Type         CredentialType `json:"type"`
	Header       string         `json:"header,omitempty"`
	Value        string         `json:"value,omitempty"`
	Token        string         `json:"token,omitempty"`
	Username     string         `json:"username,omitempty"`
	Password     string         `json:"password,omitempty"`
	AuthURL      string         `json:"auth_url,omitempty"`
	ClientID     string         `json:"client_id,omitempty"`
	ClientSecret string         `json:"client_secret,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	TokenURL     string         `json:"token_url,omitempty"`
	AccessToken  string         `json:"access_token,omitempty"`
	RefreshToken string         `json:"refresh_token,omitempty"`
	TokenExpiry  string         `json:"token_expiry,omitempty"`
}

// OAuthTokenFetcher fetches an OAuth access token for a credential.
// Used by ResolveCredentialHeader for OAuth credential types.
// The implementation varies by backend (cached vs. direct fetch).
type OAuthTokenFetcher func(cred *Credential) (accessToken string, err error)

// ResolveCredentialHeader resolves a credential to an HTTP header key/value pair.
// This is the shared logic used by both file-based and PG credential stores.
//
// For OAuth credential types, the provided OAuthTokenFetcher is called to obtain
// the access token (allowing each backend to implement its own caching strategy).
// If oauthFetcher is nil, OAuth credentials will return an error.
func ResolveCredentialHeader(name string, cred *Credential, oauthFetcher OAuthTokenFetcher) (headerKey, headerValue string, err error) {
	if cred == nil {
		return "", "", fmt.Errorf("credential %q not found", name)
	}

	switch cred.Type {
	case CredAPIKey:
		header := cred.Header
		if header == "" {
			header = "Authorization"
		}
		return header, cred.Value, nil

	case CredBearer:
		return "Authorization", "Bearer " + cred.Token, nil

	case CredBasic:
		encoded := base64.StdEncoding.EncodeToString([]byte(cred.Username + ":" + cred.Password))
		return "Authorization", "Basic " + encoded, nil

	case CredOAuthClientCreds:
		if oauthFetcher == nil {
			return "", "", fmt.Errorf("credential %q: OAuth client_credentials requires a token fetcher", name)
		}
		token, err := oauthFetcher(cred)
		if err != nil {
			return "", "", fmt.Errorf("credential %q OAuth: %w", name, err)
		}
		return "Authorization", "Bearer " + token, nil

	case CredOAuthAuthCode:
		if cred.AccessToken != "" {
			return "Authorization", "Bearer " + cred.AccessToken, nil
		}
		if oauthFetcher != nil {
			token, err := oauthFetcher(cred)
			if err != nil {
				return "", "", fmt.Errorf("credential %q OAuth: %w", name, err)
			}
			return "Authorization", "Bearer " + token, nil
		}
		return "", "", fmt.Errorf("credential %q: no access token available (OAuth authorization_code flow requires token refresh)", name)

	case CredPassword:
		return "", "", fmt.Errorf("credential %q is a password credential (for SSH/FTP/etc.), not an HTTP credential — use resolve_credential to access its fields", name)

	default:
		return "", "", fmt.Errorf("credential %q: unsupported type %q", name, cred.Type)
	}
}

// CredentialStore manages encrypted credentials and secrets.
//
// The interface covers both named credentials (for HTTP auth, OAuth, etc.)
// and arbitrary key-value secrets (for API keys, tokens, etc.).
type CredentialStore interface {
	// Credential CRUD.
	Get(name string) *Credential
	Set(name string, cred *Credential) error
	Remove(name string) error
	List() map[string]CredentialType
	Count() int

	// Credential resolution for HTTP requests.
	Resolve(name string) (headerKey, headerValue string, err error)

	// Secret key-value store (for API keys, tokens, etc.).
	SetSecret(key, value string) error
	SetSecretBatch(secrets map[string]string) error
	GetSecret(key string) string
	RemoveSecret(key string) error
	HasSecrets() bool
	SecretCount() int
	ListSecrets() []string

	// Reload re-reads credentials from the backing store.
	Reload() error
}
