package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/schardosin/astonish/pkg/store"
)

// sqliteCredentialStore implements store.CredentialStore.
// Credentials are stored as JSON in the encrypted BLOB column.
// Note: encryption is handled at the application level before storage.
type sqliteCredentialStore struct {
	db *sql.DB
}

func (s *sqliteCredentialStore) Get(ctx context.Context, name string) *store.Credential {
	var data []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT encrypted FROM credentials WHERE name = ?`, name).Scan(&data)
	if err != nil {
		return nil
	}
	cred := &store.Credential{}
	if err := json.Unmarshal(data, cred); err != nil {
		return nil
	}
	return cred
}

func (s *sqliteCredentialStore) Set(ctx context.Context, name string, cred *store.Credential) error {
	data, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	id := uuid.New().String()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO credentials (id, name, cred_type, encrypted, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, '', datetime('now'), datetime('now'))
		 ON CONFLICT(name) DO UPDATE SET cred_type = excluded.cred_type, encrypted = excluded.encrypted, updated_at = datetime('now')`,
		id, name, cred.Type, data)
	return err
}

func (s *sqliteCredentialStore) Remove(ctx context.Context, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM credentials WHERE name = ?`, name)
	return err
}

func (s *sqliteCredentialStore) List(ctx context.Context) map[string]store.CredentialType {
	rows, err := s.db.QueryContext(ctx, `SELECT name, cred_type FROM credentials ORDER BY name`)
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

func (s *sqliteCredentialStore) Count(ctx context.Context) int {
	var count int
	s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM credentials`).Scan(&count)
	return count
}

func (s *sqliteCredentialStore) Resolve(ctx context.Context, name string) (headerKey, headerValue string, err error) {
	cred := s.Get(ctx, name)
	return store.ResolveCredentialHeader(name, cred, nil)
}

// Secret storage: secrets are stored as credentials with type "secret".

func (s *sqliteCredentialStore) SetSecret(ctx context.Context, key, value string) error {
	cred := &store.Credential{Type: "secret", Value: value}
	return s.Set(ctx, "_secret_"+key, cred)
}

func (s *sqliteCredentialStore) SetSecretBatch(ctx context.Context, secrets map[string]string) error {
	for k, v := range secrets {
		if err := s.SetSecret(ctx, k, v); err != nil {
			return err
		}
	}
	return nil
}

func (s *sqliteCredentialStore) GetSecret(ctx context.Context, key string) string {
	cred := s.Get(ctx, "_secret_"+key)
	if cred == nil {
		return ""
	}
	return cred.Value
}

func (s *sqliteCredentialStore) RemoveSecret(ctx context.Context, key string) error {
	return s.Remove(ctx, "_secret_"+key)
}

func (s *sqliteCredentialStore) HasSecrets(ctx context.Context) bool {
	var count int
	s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM credentials WHERE name LIKE '_secret_%'`).Scan(&count)
	return count > 0
}

func (s *sqliteCredentialStore) SecretCount(ctx context.Context) int {
	var count int
	s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM credentials WHERE name LIKE '_secret_%'`).Scan(&count)
	return count
}

func (s *sqliteCredentialStore) ListSecrets(ctx context.Context) []string {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM credentials WHERE name LIKE '_secret_%' ORDER BY name`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		// Strip prefix
		keys = append(keys, name[8:]) // len("_secret_") == 8
	}
	return keys
}

func (s *sqliteCredentialStore) Reload(_ context.Context) error {
	return nil // No-op; SQLite reads are always fresh
}
