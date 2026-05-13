package pgstore

import (
	"context"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
)

// OrgEncryptionKeyManager manages per-org data encryption keys (DEKs).
//
// Envelope encryption scheme:
//   - Master key (KEK): loaded from ASTONISH_MASTER_KEY env var or .store_key file
//   - DEK: per-org 256-bit AES key stored in org_encryption_keys table, encrypted by master key
//   - Credential data: encrypted by DEK at the application level (pgCredentialStore)
//
// If no master key is available from either source, encryption is disabled.
type OrgEncryptionKeyManager struct {
	pool      *pgxpool.Pool
	masterKey []byte // nil if env var not set (encryption disabled)
}

// NewOrgEncryptionKeyManager creates a key manager for the given org pool.
// It reads the master key from (in priority order):
//  1. ASTONISH_MASTER_KEY environment variable (for production/k8s deployments)
//  2. The .store_key file in the config directory (auto-generated, zero-config)
//
// If neither source provides a valid key, encryption is disabled.
func NewOrgEncryptionKeyManager(pool *pgxpool.Pool) *OrgEncryptionKeyManager {
	mgr := &OrgEncryptionKeyManager{pool: pool}

	// Priority 1: environment variable (production deployments, external secret injection)
	masterKeyHex := os.Getenv("ASTONISH_MASTER_KEY")

	// Priority 2: .store_key file (auto-generated on first daemon start)
	if masterKeyHex == "" {
		if configDir, err := config.GetConfigDir(); err == nil {
			keyPath := filepath.Join(configDir, ".store_key")
			if data, err := os.ReadFile(keyPath); err == nil {
				masterKeyHex = strings.TrimSpace(string(data))
			}
		}
	}

	if masterKeyHex != "" {
		key, err := hex.DecodeString(masterKeyHex)
		if err != nil || len(key) != 32 {
			// Invalid master key — log warning and disable encryption
			fmt.Fprintf(os.Stderr, "WARNING: master key is invalid (expected 64 hex chars for 256-bit key), credential encryption disabled\n")
			return mgr
		}
		mgr.masterKey = key
	}

	return mgr
}

// GetOrCreateCredentialKey returns the org's credential encryption key (DEK).
// If no key exists yet, a new one is generated and stored (encrypted by master key).
// Returns nil if encryption is disabled (no master key configured).
func (m *OrgEncryptionKeyManager) GetOrCreateCredentialKey(ctx context.Context) ([]byte, error) {
	if m.masterKey == nil {
		return nil, nil // encryption disabled
	}

	// Try to load existing key
	var encryptedKey []byte
	err := m.pool.QueryRow(ctx,
		`SELECT key_data FROM org_encryption_keys WHERE key_name = 'credential_key'`,
	).Scan(&encryptedKey)

	if err == nil {
		// Decrypt DEK with master key
		dek, decErr := credentials.Decrypt(encryptedKey, m.masterKey)
		if decErr != nil {
			return nil, fmt.Errorf("decrypt org credential key: %w", decErr)
		}
		return dek, nil
	}

	// Key doesn't exist — generate and store
	dek, err := credentials.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("generate org credential key: %w", err)
	}

	// Encrypt DEK with master key
	encryptedDEK, err := credentials.Encrypt(dek, m.masterKey)
	if err != nil {
		return nil, fmt.Errorf("encrypt org credential key: %w", err)
	}

	// Store in database
	_, err = m.pool.Exec(ctx,
		`INSERT INTO org_encryption_keys (key_name, key_data) VALUES ('credential_key', $1)
		 ON CONFLICT (key_name) DO NOTHING`,
		encryptedDEK,
	)
	if err != nil {
		return nil, fmt.Errorf("store org credential key: %w", err)
	}

	return dek, nil
}

// EncryptionEnabled returns true if a master key is configured.
func (m *OrgEncryptionKeyManager) EncryptionEnabled() bool {
	return m.masterKey != nil
}
