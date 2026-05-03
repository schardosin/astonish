package config

import (
	"encoding/hex"
	"os"
	"testing"
)

func TestGenerateJWTSecret_Length(t *testing.T) {
	secret := GenerateJWTSecret()
	if len(secret) != 64 {
		t.Errorf("GenerateJWTSecret() length = %d, want 64", len(secret))
	}
}

func TestGenerateJWTSecret_HexFormat(t *testing.T) {
	secret := GenerateJWTSecret()
	_, err := hex.DecodeString(secret)
	if err != nil {
		t.Errorf("GenerateJWTSecret() is not valid hex: %v", err)
	}
}

func TestGenerateJWTSecret_Uniqueness(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		secret := GenerateJWTSecret()
		if seen[secret] {
			t.Fatalf("GenerateJWTSecret() produced duplicate on iteration %d", i)
		}
		seen[secret] = true
	}
}

func TestGenerateJWTSecret_Entropy(t *testing.T) {
	// Ensure the decoded bytes are actually 32 bytes (256 bits of entropy)
	secret := GenerateJWTSecret()
	decoded, err := hex.DecodeString(secret)
	if err != nil {
		t.Fatalf("hex decode failed: %v", err)
	}
	if len(decoded) != 32 {
		t.Errorf("decoded length = %d bytes, want 32", len(decoded))
	}
	// Check that at least some bytes are non-zero (extremely unlikely to fail)
	allZero := true
	for _, b := range decoded {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("all 32 bytes are zero — extremely unlikely with crypto/rand")
	}
}

func TestGetJWTSecret_FromConfig(t *testing.T) {
	cfg := &PlatformAuthConfig{JWTSecret: "config-secret-value"}
	got := cfg.GetJWTSecret()
	if got != "config-secret-value" {
		t.Errorf("GetJWTSecret() = %q, want %q", got, "config-secret-value")
	}
}

func TestGetJWTSecret_FallbackToEnv(t *testing.T) {
	cfg := &PlatformAuthConfig{JWTSecret: ""}

	// Set env var
	os.Setenv("ASTONISH_JWT_SECRET", "env-secret-value")
	defer os.Unsetenv("ASTONISH_JWT_SECRET")

	got := cfg.GetJWTSecret()
	if got != "env-secret-value" {
		t.Errorf("GetJWTSecret() = %q, want %q", got, "env-secret-value")
	}
}

func TestGetJWTSecret_ConfigPrecedence(t *testing.T) {
	cfg := &PlatformAuthConfig{JWTSecret: "from-config"}

	os.Setenv("ASTONISH_JWT_SECRET", "from-env")
	defer os.Unsetenv("ASTONISH_JWT_SECRET")

	got := cfg.GetJWTSecret()
	if got != "from-config" {
		t.Errorf("GetJWTSecret() = %q, want config value %q", got, "from-config")
	}
}

func TestGetJWTSecret_Empty(t *testing.T) {
	// Ensure no env var is set
	os.Unsetenv("ASTONISH_JWT_SECRET")

	cfg := &PlatformAuthConfig{JWTSecret: ""}
	got := cfg.GetJWTSecret()
	if got != "" {
		t.Errorf("GetJWTSecret() = %q, want empty", got)
	}
}
