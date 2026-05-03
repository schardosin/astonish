package store

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
