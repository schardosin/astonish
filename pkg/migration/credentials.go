package migration

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/store"
)

// credentialFileData is the decrypted content of credentials.enc.
type credentialFileData struct {
	Credentials map[string]*fileCredential `json:"credentials"`
	Secrets     map[string]string          `json:"secrets,omitempty"`
}

// fileCredential matches the on-disk credential format from pkg/credentials.
type fileCredential struct {
	Type         string `json:"type"`
	Header       string `json:"header,omitempty"`
	Value        string `json:"value,omitempty"`
	Token        string `json:"token,omitempty"`
	Username     string `json:"username,omitempty"`
	Password     string `json:"password,omitempty"`
	AuthURL      string `json:"auth_url,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
	Scope        string `json:"scope,omitempty"`
	TokenURL     string `json:"token_url,omitempty"`
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenExpiry  string `json:"token_expiry,omitempty"`
}

func (m *Migrator) migrateCredentials(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	encPath := filepath.Join(m.configDir, "credentials.enc")
	keyPath := filepath.Join(m.configDir, ".store_key")

	// Check if credential file exists
	if _, err := os.Stat(encPath); os.IsNotExist(err) {
		m.emitProgress(CatCredentials, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatCredentials, 0, 0, "counting", "")

	// Read the encryption key
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		m.emitProgress(CatCredentials, 0, 0, "error", "cannot read .store_key")
		return 0, fmt.Errorf("cannot read store key: %w", err)
	}

	key, err := parseStoreKey(keyData)
	if err != nil {
		m.emitProgress(CatCredentials, 0, 0, "error", "invalid store key format")
		return 0, fmt.Errorf("invalid store key: %w", err)
	}

	// Read and decrypt the credential file
	encrypted, err := os.ReadFile(encPath)
	if err != nil {
		m.emitProgress(CatCredentials, 0, 0, "error", "cannot read credentials.enc")
		return 0, fmt.Errorf("cannot read credentials.enc: %w", err)
	}

	decrypted, err := decryptAESGCM(key, encrypted)
	if err != nil {
		m.emitProgress(CatCredentials, 0, 0, "error", "decryption failed")
		return 0, fmt.Errorf("decryption failed: %w", err)
	}

	// Parse the JSON content
	var data credentialFileData
	if err := json.Unmarshal(decrypted, &data); err != nil {
		m.emitProgress(CatCredentials, 0, 0, "error", "invalid credential data format")
		return 0, fmt.Errorf("invalid credential format: %w", err)
	}

	total := len(data.Credentials) + len(data.Secrets)
	m.emitProgress(CatCredentials, 0, total, "migrating", "")

	// Get the credential store from the team data store
	credStore := teamDS.Credentials()

	// Get platform secret store for instance-wide secrets
	platformSecrets := m.pgStore.PlatformSecrets()

	count := 0

	// Migrate credentials (these are team-scoped: provider API keys, etc.)
	for name, fc := range data.Credentials {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		cred := &store.Credential{
			Type:         fc.Type,
			Header:       fc.Header,
			Value:        fc.Value,
			Token:        fc.Token,
			Username:     fc.Username,
			Password:     fc.Password,
			AuthURL:      fc.AuthURL,
			ClientID:     fc.ClientID,
			ClientSecret: fc.ClientSecret,
			Scope:        fc.Scope,
			TokenURL:     fc.TokenURL,
			AccessToken:  fc.AccessToken,
			RefreshToken: fc.RefreshToken,
			TokenExpiry:  fc.TokenExpiry,
		}

		if err := credStore.Set(name, cred); err != nil {
			return count, fmt.Errorf("failed to migrate credential %q: %w", name, err)
		}
		count++
		m.emitProgress(CatCredentials, count, total, "migrating", "")
	}

	// Migrate secrets — route platform-level secrets to platform_secrets table,
	// everything else to the team credential store.
	for key, value := range data.Secrets {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		if isPlatformSecret(key) {
			// Instance-wide secret → platform_secrets table
			if err := platformSecrets.SetSecret(key, value); err != nil {
				return count, fmt.Errorf("failed to migrate platform secret %q: %w", key, err)
			}
		} else {
			// Team-scoped secret → team credential store
			if err := credStore.SetSecret(key, value); err != nil {
				return count, fmt.Errorf("failed to migrate secret %q: %w", key, err)
			}
		}
		count++
		m.emitProgress(CatCredentials, count, total, "migrating", "")
	}

	m.emitProgress(CatCredentials, count, total, "done", "")
	return count, nil
}

// isPlatformSecret returns true if the secret key is an instance-wide platform
// secret (not scoped to any org/team). These are stored in the platform_secrets
// table rather than the team credential store.
func isPlatformSecret(key string) bool {
	platformPrefixes := []string{
		"channels.",       // channels.telegram.bot_token, channels.email.password
		"web_servers.",    // web_servers.tavily.api_key, etc.
		"memory.embedding.", // memory.embedding.api_key
	}
	for _, prefix := range platformPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

// parseStoreKey reads the store key from file content.
// Supports both hex-encoded (64 chars + optional newline) and raw binary (32 bytes).
func parseStoreKey(data []byte) ([]byte, error) {
	// Try hex format first (modern format)
	trimmed := strings.TrimSpace(string(data))
	if len(trimmed) == 64 {
		key, err := hex.DecodeString(trimmed)
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}

	// Fallback to raw binary (legacy format)
	if len(data) == 32 {
		return data, nil
	}

	return nil, fmt.Errorf("store key must be 32 bytes (raw) or 64 hex chars, got %d bytes", len(data))
}
