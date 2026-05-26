package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
)

// SQLitePlatformSecretStore manages instance-wide secrets stored in the platform
// database's platform_secrets table. Encryption uses AES-256-GCM with the
// master key (same approach as pgstore.PlatformSecretStore).
type SQLitePlatformSecretStore struct {
	db        *sql.DB
	masterKey []byte // nil = encryption disabled (plaintext fallback)
}

// NewSQLitePlatformSecretStore creates a store for platform-level secrets.
func NewSQLitePlatformSecretStore(db *sql.DB) *SQLitePlatformSecretStore {
	return &SQLitePlatformSecretStore{
		db:        db,
		masterKey: loadSQLiteMasterKey(),
	}
}

// GetSecret retrieves and decrypts a platform secret by key.
// Returns empty string if not found or decryption fails.
func (s *SQLitePlatformSecretStore) GetSecret(key string) string {
	var encrypted []byte
	err := s.db.QueryRowContext(context.Background(),
		`SELECT value FROM platform_secrets WHERE key = ?`, key,
	).Scan(&encrypted)
	if err != nil {
		return ""
	}

	plaintext, err := s.decrypt(encrypted)
	if err != nil {
		slog.Warn("failed to decrypt platform secret", "key", key, "error", err)
		return ""
	}
	return string(plaintext)
}

// SetSecret encrypts and stores a platform secret (upsert).
func (s *SQLitePlatformSecretStore) SetSecret(key, value string) error {
	encrypted, err := s.encrypt([]byte(value))
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(context.Background(),
		`INSERT INTO platform_secrets (key, value, created_at, updated_at)
		 VALUES (?, ?, datetime('now'), datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = datetime('now')`,
		key, encrypted,
	)
	return err
}

// RemoveSecret deletes a platform secret.
func (s *SQLitePlatformSecretStore) RemoveSecret(key string) error {
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM platform_secrets WHERE key = ?`, key)
	return err
}

// ListSecrets returns all platform secret keys (without values).
func (s *SQLitePlatformSecretStore) ListSecrets() []string {
	rows, err := s.db.QueryContext(context.Background(),
		`SELECT key FROM platform_secrets ORDER BY key`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err == nil {
			keys = append(keys, k)
		}
	}
	return keys
}

func (s *SQLitePlatformSecretStore) encrypt(plaintext []byte) ([]byte, error) {
	if len(s.masterKey) == 0 {
		// No master key — store plaintext (development/testing fallback)
		return plaintext, nil
	}
	return credentials.Encrypt(plaintext, s.masterKey)
}

func (s *SQLitePlatformSecretStore) decrypt(data []byte) ([]byte, error) {
	if len(s.masterKey) == 0 {
		return data, nil
	}
	plaintext, err := credentials.Decrypt(data, s.masterKey)
	if err != nil {
		// Fallback: data might be unencrypted (stored before encryption was enabled)
		if isPrintableText(data) {
			return data, nil
		}
		return nil, err
	}
	return plaintext, nil
}

// isPrintableText checks if data looks like unencrypted text (ASCII/UTF-8 printable).
func isPrintableText(data []byte) bool {
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return len(data) > 0
}

// loadSQLiteMasterKey reads the master key from env or .store_key file.
// Returns nil if no valid key is found (encryption disabled).
func loadSQLiteMasterKey() []byte {
	// Priority 1: environment variable
	masterKeyHex := os.Getenv("ASTONISH_MASTER_KEY")

	// Priority 2: .store_key file in config directory
	if masterKeyHex == "" {
		if configDir, err := config.GetConfigDir(); err == nil {
			keyPath := filepath.Join(configDir, ".store_key")
			if data, err := os.ReadFile(keyPath); err == nil {
				masterKeyHex = strings.TrimSpace(string(data))
			}
		}
	}

	if masterKeyHex == "" {
		return nil
	}

	key, err := hex.DecodeString(masterKeyHex)
	if err != nil || len(key) != 32 {
		slog.Warn("ASTONISH_MASTER_KEY invalid (must be 64-char hex / 32 bytes), encryption disabled")
		return nil
	}
	return key
}
