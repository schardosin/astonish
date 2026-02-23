package credentials

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// CredentialType defines the authentication mechanism.
type CredentialType string

const (
	// CredAPIKey uses a custom header with a static value.
	// Example: X-API-Key: my-secret-key
	CredAPIKey CredentialType = "api_key"

	// CredBearer uses Authorization: Bearer <token>.
	CredBearer CredentialType = "bearer"

	// CredBasic uses Authorization: Basic <base64(user:pass)>.
	CredBasic CredentialType = "basic"

	// CredOAuthClientCreds performs OAuth2 client_credentials flow
	// and produces Authorization: Bearer <access_token>.
	CredOAuthClientCreds CredentialType = "oauth_client_credentials"

	// CredPassword stores a plain username + password pair for non-HTTP use
	// cases: SSH, FTP, SMTP, databases, etc. Unlike basic, this type is not
	// an HTTP credential — Resolve() will return an error. Use Get() or the
	// resolve_credential LLM tool to access the raw fields.
	CredPassword CredentialType = "password"
)

// Credential holds authentication data for a single named credential.
type Credential struct {
	Type CredentialType `json:"type"`

	// api_key fields
	Header string `json:"header,omitempty"` // e.g., "X-API-Key", "Authorization"
	Value  string `json:"value,omitempty"`  // the raw key/token value

	// bearer fields
	Token string `json:"token,omitempty"`

	// basic fields
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`

	// oauth_client_credentials fields
	AuthURL      string `json:"auth_url,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Scope        string `json:"scope,omitempty"`
}

// storeData is the JSON structure inside the encrypted file.
type storeData struct {
	Credentials map[string]*Credential `json:"credentials"`
	Secrets     map[string]string      `json:"secrets,omitempty"`  // flat key-value store (provider keys, tokens, etc.)
	Migrated    bool                   `json:"migrated,omitempty"` // true after config.yaml secrets have been migrated
}

// Store manages encrypted credential persistence with integrated redaction.
type Store struct {
	configDir string
	key       []byte
	mu        sync.RWMutex
	data      *storeData
	redactor  *Redactor
	tokens    *tokenCache
}

// Open loads (or creates) the credential store for the given config directory.
// The encryption key is loaded from .store_key (auto-generated on first use).
// The encrypted credentials are loaded from credentials.enc.
func Open(configDir string) (*Store, error) {
	key, err := loadOrCreateKey(configDir)
	if err != nil {
		return nil, fmt.Errorf("credential store key: %w", err)
	}

	s := &Store{
		configDir: configDir,
		key:       key,
		data: &storeData{
			Credentials: make(map[string]*Credential),
			Secrets:     make(map[string]string),
		},
		redactor: NewRedactor(),
		tokens:   newTokenCache(),
	}

	// Register the encryption key itself for redaction.
	// Both raw bytes (if somehow cat'd) and hex-encoded form.
	hexKey := hex.EncodeToString(key)
	s.redactor.AddSecret(storeKeyRedactName, hexKey)
	s.redactor.AddSecret(storeKeyRedactName, string(key))

	// Load existing store
	if err := s.load(); err != nil {
		return nil, err
	}

	// Build redaction signatures from loaded credentials
	s.redactor.UpdateFromCredentials(s.data.Credentials)

	// Register flat secrets for redaction
	for key, val := range s.data.Secrets {
		s.redactor.AddSecret("secret/"+key, val)
	}

	return s, nil
}

// Reload re-reads the credential store from disk, picking up changes made by
// other processes (e.g., the CLI running "astonish credential add" while the
// daemon is running). The in-memory state and redaction signatures are fully
// replaced. This is safe to call concurrently with other Store methods.
func (s *Store) Reload() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.load(); err != nil {
		return fmt.Errorf("reload credential store: %w", err)
	}

	// Rebuild redaction signatures from refreshed data
	s.redactor.UpdateFromCredentials(s.data.Credentials)
	for key, val := range s.data.Secrets {
		s.redactor.AddSecret("secret/"+key, val)
	}

	return nil
}

// Get returns a copy of the named credential, or nil if not found.
func (s *Store) Get(name string) *Credential {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cred, ok := s.data.Credentials[name]
	if !ok {
		return nil
	}
	cp := *cred
	return &cp
}

// Set adds or updates a credential and persists the store.
func (s *Store) Set(name string, cred *Credential) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Credentials[name] = cred
	if err := s.save(); err != nil {
		return err
	}

	s.redactor.UpdateFromCredentials(s.data.Credentials)
	return nil
}

// Remove deletes a credential and persists the store.
func (s *Store) Remove(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Credentials[name]; !ok {
		return fmt.Errorf("credential %q not found", name)
	}

	delete(s.data.Credentials, name)
	if err := s.save(); err != nil {
		return err
	}

	s.redactor.RemoveByName(name)
	s.redactor.UpdateFromCredentials(s.data.Credentials)
	return nil
}

// List returns credential names and their types. No secret values are exposed.
func (s *Store) List() map[string]CredentialType {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make(map[string]CredentialType, len(s.data.Credentials))
	for name, cred := range s.data.Credentials {
		result[name] = cred.Type
	}
	return result
}

// Count returns the number of stored credentials.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Credentials)
}

// Resolve produces the HTTP header key and value for a named credential.
// For OAuth credentials, it handles token acquisition and caching automatically.
// The resolved credential values are never exposed to the LLM — they are
// injected directly into HTTP requests by the http_request tool.
func (s *Store) Resolve(name string) (headerKey, headerValue string, err error) {
	s.mu.RLock()
	cred, ok := s.data.Credentials[name]
	if !ok {
		s.mu.RUnlock()
		return "", "", fmt.Errorf("credential %q not found", name)
	}
	// Copy to avoid holding the lock during OAuth calls
	credCopy := *cred
	s.mu.RUnlock()

	switch credCopy.Type {
	case CredAPIKey:
		if credCopy.Header == "" {
			return "", "", fmt.Errorf("credential %q: api_key type requires a header name", name)
		}
		return credCopy.Header, credCopy.Value, nil

	case CredBearer:
		return "Authorization", "Bearer " + credCopy.Token, nil

	case CredBasic:
		encoded := basicAuthValue(credCopy.Username, credCopy.Password)
		return "Authorization", "Basic " + encoded, nil

	case CredOAuthClientCreds:
		token, err := s.tokens.GetOrRefresh(name, &credCopy, s.redactor)
		if err != nil {
			return "", "", fmt.Errorf("credential %q OAuth: %w", name, err)
		}
		return "Authorization", "Bearer " + token, nil

	case CredPassword:
		return "", "", fmt.Errorf("credential %q is a password credential (for SSH/FTP/etc.), not an HTTP credential — use resolve_credential to access its fields", name)

	default:
		return "", "", fmt.Errorf("credential %q: unknown type %q", name, credCopy.Type)
	}
}

// Redactor returns the redaction engine for wiring into tool outputs,
// channel delivery, and session storage.
func (s *Store) Redactor() *Redactor {
	return s.redactor
}

// --- Flat secret storage (for provider keys, bot tokens, etc.) ---

// SetSecret stores a single key-value secret in the encrypted store.
// The key uses dot notation (e.g., "provider.anthropic.api_key").
// The value is automatically registered for redaction.
func (s *Store) SetSecret(key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Secrets[key] = value
	if err := s.save(); err != nil {
		return err
	}
	s.redactor.AddSecret("secret/"+key, value)
	return nil
}

// SetSecretBatch stores multiple key-value secrets in a single encrypted write.
// More efficient than calling SetSecret in a loop.
func (s *Store) SetSecretBatch(secrets map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, value := range secrets {
		s.data.Secrets[key] = value
	}
	if err := s.save(); err != nil {
		return err
	}
	for key, value := range secrets {
		s.redactor.AddSecret("secret/"+key, value)
	}
	return nil
}

// GetSecret retrieves a flat secret value by key.
// Returns empty string if not found.
func (s *Store) GetSecret(key string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Secrets[key]
}

// RemoveSecret deletes a flat secret and persists the store.
func (s *Store) RemoveSecret(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Secrets[key]; !ok {
		return nil // not an error, just a no-op
	}
	delete(s.data.Secrets, key)
	if err := s.save(); err != nil {
		return err
	}
	s.redactor.RemoveByName("secret/" + key)
	return nil
}

// HasSecrets returns true if any flat secrets are stored.
func (s *Store) HasSecrets() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Secrets) > 0
}

// SecretCount returns the number of flat secrets stored.
func (s *Store) SecretCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.data.Secrets)
}

// ListSecrets returns the keys of all flat secrets (no values).
// Keys use dot notation, e.g. "provider.anthropic.api_key".
func (s *Store) ListSecrets() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.data.Secrets))
	for key := range s.data.Secrets {
		keys = append(keys, key)
	}
	return keys
}

// --- Migration tracking ---

// HasMigrated returns true if config.yaml secrets have been migrated
// to the credential store. Once true, migration is skipped on future startups.
func (s *Store) HasMigrated() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Migrated
}

// SetMigrated marks the credential store as having completed config migration.
func (s *Store) SetMigrated() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.data.Migrated = true
	return s.save()
}

// load reads and decrypts the credential store from disk.
func (s *Store) load() error {
	path := storeFilePath(s.configDir)

	ciphertext, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil // Empty store, first run
	}
	if err != nil {
		return fmt.Errorf("read credential store: %w", err)
	}
	if len(ciphertext) == 0 {
		return nil
	}

	plaintext, err := decrypt(ciphertext, s.key)
	if err != nil {
		return fmt.Errorf("decrypt credential store: %w", err)
	}

	var data storeData
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return fmt.Errorf("parse credential store: %w", err)
	}
	if data.Credentials == nil {
		data.Credentials = make(map[string]*Credential)
	}
	if data.Secrets == nil {
		data.Secrets = make(map[string]string)
	}

	s.data = &data
	return nil
}

// save encrypts and writes the credential store to disk atomically.
func (s *Store) save() error {
	plaintext, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal credential store: %w", err)
	}

	ciphertext, err := encrypt(plaintext, s.key)
	if err != nil {
		return fmt.Errorf("encrypt credential store: %w", err)
	}

	path := storeFilePath(s.configDir)
	tmp := path + ".tmp"

	if err := os.WriteFile(tmp, ciphertext, 0600); err != nil {
		return fmt.Errorf("write temp credential store: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename credential store: %w", err)
	}

	return nil
}

// basicAuthValue encodes username:password as base64 for HTTP Basic auth.
func basicAuthValue(username, password string) string {
	auth := username + ":" + password
	return base64.StdEncoding.EncodeToString([]byte(auth))
}
