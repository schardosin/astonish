package pgstore

import (
	"context"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
)

// PlatformSecretStore manages instance-wide secrets stored in the platform
// database's platform_secrets table. These are not org/team-scoped — they hold
// infrastructure secrets like the Telegram bot token.
//
// Encryption uses the master key directly (AES-256-GCM), without per-org DEK
// indirection, since platform secrets have no org boundary.
type PlatformSecretStore struct {
	poolMgr   *PoolManager
	masterKey []byte // nil = encryption disabled
}

// NewPlatformSecretStore creates a store for platform-level secrets.
// Reads the master key from ASTONISH_MASTER_KEY env or .store_key file.
func NewPlatformSecretStore(poolMgr *PoolManager) *PlatformSecretStore {
	return &PlatformSecretStore{
		poolMgr:   poolMgr,
		masterKey: loadMasterKey(),
	}
}

// GetSecret retrieves and decrypts a platform secret by key.
// Returns empty string if not found or decryption fails.
func (s *PlatformSecretStore) GetSecret(key string) string {
	ctx := context.Background()
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return ""
	}

	var encrypted []byte
	err = pool.QueryRow(ctx,
		`SELECT value FROM platform_secrets WHERE key = $1`, key,
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
func (s *PlatformSecretStore) SetSecret(key, value string) error {
	ctx := context.Background()
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}

	encrypted, err := s.encrypt([]byte(value))
	if err != nil {
		return fmt.Errorf("encrypt platform secret: %w", err)
	}

	_, err = pool.Exec(ctx,
		`INSERT INTO platform_secrets (key, value, created_at, updated_at)
		 VALUES ($1, $2, NOW(), NOW())
		 ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = NOW()`,
		key, encrypted,
	)
	return err
}

// RemoveSecret deletes a platform secret.
func (s *PlatformSecretStore) RemoveSecret(key string) error {
	ctx := context.Background()
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return err
	}

	_, err = pool.Exec(ctx, `DELETE FROM platform_secrets WHERE key = $1`, key)
	return err
}

// ListSecrets returns all platform secret keys (without values).
func (s *PlatformSecretStore) ListSecrets() []string {
	ctx := context.Background()
	pool, err := s.poolMgr.PlatformPool(ctx)
	if err != nil {
		return nil
	}

	rows, err := pool.Query(ctx, `SELECT key FROM platform_secrets ORDER BY key`)
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

func (s *PlatformSecretStore) encrypt(plaintext []byte) ([]byte, error) {
	if len(s.masterKey) == 0 {
		return plaintext, nil
	}
	return credentials.Encrypt(plaintext, s.masterKey)
}

func (s *PlatformSecretStore) decrypt(data []byte) ([]byte, error) {
	if len(s.masterKey) == 0 {
		return data, nil
	}
	plaintext, err := credentials.Decrypt(data, s.masterKey)
	if err != nil {
		// Fallback: data might be unencrypted (stored before encryption was enabled)
		// If it looks like valid UTF-8 text, return as-is
		if isPlainText(data) {
			return data, nil
		}
		return nil, err
	}
	return plaintext, nil
}

// isPlainText checks if data looks like unencrypted text (ASCII/UTF-8 printable).
func isPlainText(data []byte) bool {
	for _, b := range data {
		if b < 0x20 && b != '\n' && b != '\r' && b != '\t' {
			return false
		}
	}
	return len(data) > 0
}

// loadMasterKey reads the master key from env or .store_key file.
// Returns nil if no valid key is found (encryption disabled).
func loadMasterKey() []byte {
	// Priority 1: environment variable
	masterKeyHex := os.Getenv("ASTONISH_MASTER_KEY")

	// Priority 2: .store_key file
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
		slog.Warn("master key is invalid, platform secret encryption disabled")
		return nil
	}
	return key
}
