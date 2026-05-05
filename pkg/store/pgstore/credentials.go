package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/store"
)

// pgCredentialStore implements store.CredentialStore for PostgreSQL.
// Credentials are encrypted at the application level with AES-256-GCM
// using the org's data encryption key (envelope encryption).
type pgCredentialStore struct {
	pool   *pgxpool.Pool
	schema string
	encKey []byte // AES-256 data encryption key (nil = no encryption, for backward compat)
	userID string // optional: set for write operations to populate created_by
}

func (c *pgCredentialStore) tableName() string {
	return pgx.Identifier{c.schema, "credentials"}.Sanitize()
}

// encrypt encrypts plaintext if an encryption key is configured.
// Falls back to raw bytes if no key is set (backward compatibility during migration).
func (c *pgCredentialStore) encrypt(plaintext []byte) ([]byte, error) {
	if len(c.encKey) == 0 {
		return plaintext, nil
	}
	return credentials.Encrypt(plaintext, c.encKey)
}

// decrypt decrypts ciphertext if an encryption key is configured.
// Falls back to treating data as raw JSON if no key is set or decryption fails
// (backward compatibility for data written before encryption was enabled).
func (c *pgCredentialStore) decrypt(data []byte) ([]byte, error) {
	if len(c.encKey) == 0 {
		return data, nil
	}
	plaintext, err := credentials.Decrypt(data, c.encKey)
	if err != nil {
		// Fallback: data might be unencrypted JSON from before encryption was enabled.
		// Try to parse as JSON directly.
		var js json.RawMessage
		if jsonErr := json.Unmarshal(data, &js); jsonErr == nil {
			slog.Debug("credential data is unencrypted, returning as-is (will be re-encrypted on next write)", "schema", c.schema)
			return data, nil
		}
		return nil, err
	}
	return plaintext, nil
}

func (c *pgCredentialStore) Get(name string) *store.Credential {
	ctx := context.Background()
	row := c.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT cred_type, encrypted FROM %s WHERE name = $1`, c.tableName()),
		name,
	)

	var credType string
	var encrypted []byte
	if err := row.Scan(&credType, &encrypted); err != nil {
		return nil
	}

	plaintext, err := c.decrypt(encrypted)
	if err != nil {
		slog.Warn("failed to decrypt credential", "name", name, "error", err)
		return nil
	}

	// Deserialize the credential from decrypted JSON
	var cred store.Credential
	if err := json.Unmarshal(plaintext, &cred); err != nil {
		// If it's not JSON, treat as raw value
		cred.Type = credType
		cred.Value = string(plaintext)
	}
	cred.Type = credType
	return &cred
}

func (c *pgCredentialStore) Set(name string, cred *store.Credential) error {
	ctx := context.Background()
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}

	encrypted, err := c.encrypt(data)
	if err != nil {
		return fmt.Errorf("encrypt credential: %w", err)
	}

	// Use created_by if available (nil for headless/system operations)
	var createdBy any
	if c.userID != "" {
		createdBy = c.userID
	}

	_, err = c.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, cred_type, encrypted, created_by, updated_at)
		 VALUES ($1, $2, $3, $4, now())
		 ON CONFLICT (name) DO UPDATE SET cred_type = $2, encrypted = $3, updated_at = now()`,
		c.tableName()),
		name, cred.Type, encrypted, createdBy,
	)
	return err
}

func (c *pgCredentialStore) Remove(name string) error {
	ctx := context.Background()
	_, err := c.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE name = $1`, c.tableName()),
		name,
	)
	return err
}

func (c *pgCredentialStore) List() map[string]store.CredentialType {
	ctx := context.Background()
	rows, err := c.pool.Query(ctx, fmt.Sprintf(
		`SELECT name, cred_type FROM %s WHERE cred_type != 'secret' ORDER BY name`, c.tableName()),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	result := make(map[string]store.CredentialType)
	for rows.Next() {
		var name, credType string
		if err := rows.Scan(&name, &credType); err != nil {
			continue
		}
		result[name] = credType
	}
	return result
}

func (c *pgCredentialStore) Count() int {
	ctx := context.Background()
	var count int
	err := c.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE cred_type != 'secret'`, c.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (c *pgCredentialStore) Resolve(name string) (headerKey, headerValue string, err error) {
	cred := c.Get(name)
	if cred == nil {
		return "", "", fmt.Errorf("credential %q not found", name)
	}
	switch cred.Type {
	case store.CredAPIKey:
		header := cred.Header
		if header == "" {
			header = "Authorization"
		}
		return header, cred.Value, nil
	case store.CredBearer:
		return "Authorization", "Bearer " + cred.Token, nil
	case store.CredBasic:
		encoded := credentials.BasicAuthValue(cred.Username, cred.Password)
		return "Authorization", "Basic " + encoded, nil
	case store.CredOAuthClientCreds:
		// Convert store.Credential to credentials.Credential for the OAuth flow
		oauthCred := &credentials.Credential{
			Type:         credentials.CredOAuthClientCreds,
			AuthURL:      cred.AuthURL,
			ClientID:     cred.ClientID,
			ClientSecret: cred.ClientSecret,
			Scope:        cred.Scope,
		}
		token, _, err := credentials.FetchOAuthToken(oauthCred)
		if err != nil {
			return "", "", fmt.Errorf("credential %q OAuth: %w", name, err)
		}
		return "Authorization", "Bearer " + token, nil
	case store.CredOAuthAuthCode:
		// For auth code flow, use the stored access token directly
		// (refresh would need additional infrastructure)
		if cred.AccessToken != "" {
			return "Authorization", "Bearer " + cred.AccessToken, nil
		}
		return "", "", fmt.Errorf("credential %q: no access token available (OAuth authorization_code flow requires token refresh)", name)
	case store.CredPassword:
		return "", "", fmt.Errorf("credential %q is a password credential (for SSH/FTP/etc.), not an HTTP credential — use resolve_credential to access its fields", name)
	default:
		return "", "", fmt.Errorf("unsupported credential type: %s", cred.Type)
	}
}

func (c *pgCredentialStore) SetSecret(key, value string) error {
	ctx := context.Background()

	encrypted, err := c.encrypt([]byte(value))
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}

	_, err = c.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, cred_type, encrypted, created_by, updated_at)
		 VALUES ($1, 'secret', $2, $3, now())
		 ON CONFLICT (name) DO UPDATE SET encrypted = $2, updated_at = now()`,
		c.tableName()),
		key, encrypted, nullableUserID(c.userID),
	)
	return err
}

func (c *pgCredentialStore) SetSecretBatch(secrets map[string]string) error {
	for k, v := range secrets {
		if err := c.SetSecret(k, v); err != nil {
			return err
		}
	}
	return nil
}

func (c *pgCredentialStore) GetSecret(key string) string {
	ctx := context.Background()
	var encrypted []byte
	err := c.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT encrypted FROM %s WHERE name = $1 AND cred_type = 'secret'`, c.tableName()),
		key,
	).Scan(&encrypted)
	if err != nil {
		return ""
	}

	plaintext, err := c.decrypt(encrypted)
	if err != nil {
		slog.Warn("failed to decrypt secret", "key", key, "error", err)
		return ""
	}
	return string(plaintext)
}

func (c *pgCredentialStore) RemoveSecret(key string) error {
	return c.Remove(key)
}

func (c *pgCredentialStore) HasSecrets() bool {
	return c.SecretCount() > 0
}

func (c *pgCredentialStore) SecretCount() int {
	ctx := context.Background()
	var count int
	err := c.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE cred_type = 'secret'`, c.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (c *pgCredentialStore) ListSecrets() []string {
	ctx := context.Background()
	rows, err := c.pool.Query(ctx, fmt.Sprintf(
		`SELECT name FROM %s WHERE cred_type = 'secret' ORDER BY name`, c.tableName()),
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var secrets []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		secrets = append(secrets, name)
	}
	return secrets
}

func (c *pgCredentialStore) Reload() error {
	// No-op for PG store — data is always read fresh from the database
	return nil
}

// nullableUserID returns nil if s is empty, otherwise returns s for use as a nullable UUID parameter.
func nullableUserID(s string) any {
	if s == "" {
		return nil
	}
	return s
}
