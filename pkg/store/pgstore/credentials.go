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

func (c *pgCredentialStore) Get(ctx context.Context, name string) *store.Credential {
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

func (c *pgCredentialStore) Set(ctx context.Context, name string, cred *store.Credential) error {
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

func (c *pgCredentialStore) Remove(ctx context.Context, name string) error {
	_, err := c.pool.Exec(ctx, fmt.Sprintf(
		`DELETE FROM %s WHERE name = $1`, c.tableName()),
		name,
	)
	return err
}

func (c *pgCredentialStore) List(ctx context.Context) map[string]store.CredentialType {
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

func (c *pgCredentialStore) Count(ctx context.Context) int {
	var count int
	err := c.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE cred_type != 'secret'`, c.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (c *pgCredentialStore) Resolve(ctx context.Context, name string) (headerKey, headerValue string, err error) {
	cred := c.Get(ctx, name)
	return store.ResolveCredentialHeader(name, cred, func(cred *store.Credential) (string, error) {
		// OAuth client_credentials: fetch token directly (no caching in pgstore)
		oauthCred := &credentials.Credential{
			Type:         credentials.CredOAuthClientCreds,
			AuthURL:      cred.AuthURL,
			ClientID:     cred.ClientID,
			ClientSecret: cred.ClientSecret,
			Scope:        cred.Scope,
		}
		token, _, err := credentials.FetchOAuthToken(oauthCred)
		return token, err
	})
}

func (c *pgCredentialStore) SetSecret(ctx context.Context, key, value string) error {
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

func (c *pgCredentialStore) SetSecretBatch(ctx context.Context, secrets map[string]string) error {
	for k, v := range secrets {
		if err := c.SetSecret(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}

func (c *pgCredentialStore) GetSecret(ctx context.Context, key string) string {
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

func (c *pgCredentialStore) RemoveSecret(ctx context.Context, key string) error {
	return c.Remove(ctx, key)
}

func (c *pgCredentialStore) HasSecrets(ctx context.Context) bool {
	return c.SecretCount(ctx) > 0
}

func (c *pgCredentialStore) SecretCount(ctx context.Context) int {
	var count int
	err := c.pool.QueryRow(ctx, fmt.Sprintf(
		`SELECT count(*) FROM %s WHERE cred_type = 'secret'`, c.tableName()),
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

func (c *pgCredentialStore) ListSecrets(ctx context.Context) []string {
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

func (c *pgCredentialStore) Reload(ctx context.Context) error {
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
