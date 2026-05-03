package migration

import (
	"crypto/aes"
	"crypto/cipher"
	"fmt"
)

// decryptAESGCM decrypts data encrypted with AES-256-GCM.
// The input format is: [12-byte nonce][ciphertext + GCM tag].
func decryptAESGCM(key, data []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("AES-256 key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize() // 12 bytes for standard GCM
	if len(data) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short: %d bytes (nonce requires %d)", len(data), nonceSize)
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("GCM decryption failed: %w", err)
	}

	return plaintext, nil
}
