package pgstore

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgCredentialStore implements store.CredentialStore for PostgreSQL.
// Credentials are stored with AES-256-GCM encrypted BYTEA.
type pgCredentialStore struct {
	pool   *pgxpool.Pool
	schema string
}

func (c *pgCredentialStore) tableName() string {
	return pgx.Identifier{c.schema, "credentials"}.Sanitize()
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

	// Deserialize the credential from the encrypted blob
	var cred store.Credential
	if err := json.Unmarshal(encrypted, &cred); err != nil {
		// If it's not JSON, treat as raw value
		cred.Type = credType
		cred.Value = string(encrypted)
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

	_, err = c.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, cred_type, encrypted, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (name) DO UPDATE SET cred_type = $2, encrypted = $3, updated_at = now()`,
		c.tableName()),
		name, cred.Type, data,
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
		`SELECT name, cred_type FROM %s ORDER BY name`, c.tableName()),
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
		`SELECT count(*) FROM %s`, c.tableName()),
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
		// Basic auth handled elsewhere
		return "", "", fmt.Errorf("basic auth requires special handling")
	default:
		return "", "", fmt.Errorf("unsupported credential type: %s", cred.Type)
	}
}

func (c *pgCredentialStore) SetSecret(key, value string) error {
	ctx := context.Background()
	_, err := c.pool.Exec(ctx, fmt.Sprintf(
		`INSERT INTO %s (name, cred_type, encrypted, updated_at)
		 VALUES ($1, 'secret', $2, now())
		 ON CONFLICT (name) DO UPDATE SET encrypted = $2, updated_at = now()`,
		c.tableName()),
		key, []byte(value),
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
	return string(encrypted)
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
