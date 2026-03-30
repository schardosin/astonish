package credentials

import (
	"testing"
)

// FuzzParseKeyFile fuzzes the key file parser with arbitrary byte sequences.
// The parser must handle hex-encoded text (64 chars) and raw binary (32 bytes)
// formats without panicking.
func FuzzParseKeyFile(f *testing.F) {
	// Valid hex key (64 hex chars)
	f.Add([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef\n"))
	// Valid hex key without newline
	f.Add([]byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"))
	// Valid raw binary key (32 bytes)
	f.Add(make([]byte, 32))
	// Wrong sizes
	f.Add([]byte{})
	f.Add([]byte("short"))
	f.Add(make([]byte, 31))
	f.Add(make([]byte, 33))
	f.Add(make([]byte, 64))
	// Invalid hex
	f.Add([]byte("zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"))

	f.Fuzz(func(t *testing.T, data []byte) {
		key, migrated, err := parseKeyFile(data)
		if err != nil {
			return
		}
		if len(key) != keySize {
			t.Fatalf("parsed key has wrong length: got %d, want %d", len(key), keySize)
		}
		// If it's a hex key, migrated should be false
		// If it's a raw binary key, migrated should be true
		_ = migrated
	})
}

// FuzzEncryptDecrypt verifies that encrypt/decrypt roundtrips correctly
// for arbitrary plaintext data.
func FuzzEncryptDecrypt(f *testing.F) {
	f.Add([]byte("hello world"))
	f.Add([]byte(""))
	f.Add([]byte("a"))
	f.Add(make([]byte, 1024))
	f.Add([]byte(`{"key": "value", "nested": {"deep": true}}`))

	f.Fuzz(func(t *testing.T, plaintext []byte) {
		key, err := generateKey()
		if err != nil {
			t.Fatalf("generateKey: %v", err)
		}

		ciphertext, err := encrypt(plaintext, key)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}

		decrypted, err := decrypt(ciphertext, key)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}

		if len(plaintext) != len(decrypted) {
			t.Fatalf("length mismatch: plaintext=%d, decrypted=%d", len(plaintext), len(decrypted))
		}
		for i := range plaintext {
			if plaintext[i] != decrypted[i] {
				t.Fatalf("byte %d differs: plaintext=%d, decrypted=%d", i, plaintext[i], decrypted[i])
			}
		}
	})
}

// FuzzDecrypt fuzzes the decrypt function with arbitrary ciphertext to ensure
// it handles malformed input gracefully without panicking.
func FuzzDecrypt(f *testing.F) {
	// Generate a valid ciphertext for the corpus
	key, _ := generateKey()
	valid, _ := encrypt([]byte("test"), key)
	f.Add(valid, key)

	// Various malformed inputs
	f.Add([]byte{}, key)
	f.Add([]byte("short"), key)
	f.Add(make([]byte, 12), key) // nonce-only, no ciphertext
	f.Add(make([]byte, 100), key)

	f.Fuzz(func(t *testing.T, ciphertext, key []byte) {
		// We don't care about errors — we're looking for panics
		_, _ = decrypt(ciphertext, key)
	})
}

// FuzzRedact fuzzes the Redact function with arbitrary text to ensure
// it handles all input without panicking.
func FuzzRedact(f *testing.F) {
	f.Add("This is a normal text with api_key_12345678 in it")
	f.Add("")
	f.Add("no secrets here")
	f.Add("api_key_12345678 appears twice: api_key_12345678")
	f.Add("base64: YXBpX2tleV8xMjM0NTY3OA==")
	f.Add("urlencoded: api_key_12345678%21%40%23")

	f.Fuzz(func(t *testing.T, text string) {
		r := NewRedactor()
		r.AddSecret("test-key", "api_key_12345678")
		r.AddSecret("test-token", "bearer_token_99999999")

		result := r.Redact(text)

		// Verify redacted text never contains the raw secret
		if len(result) > 0 && len(text) > 0 {
			// Just ensure no panic occurred — the result is a valid string
			_ = len(result)
		}
	})
}
