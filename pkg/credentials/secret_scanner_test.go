package credentials

import (
	"strings"
	"testing"
)

func TestSecretScanner_Disabled(t *testing.T) {
	s := NewSecretScanner()
	s.Enabled = false
	detections := s.Scan("password=hunter2")
	if len(detections) != 0 {
		t.Errorf("expected 0 detections when disabled, got %d", len(detections))
	}
}

func TestSecretScanner_EmptyText(t *testing.T) {
	s := NewSecretScanner()
	detections := s.Scan("")
	if len(detections) != 0 {
		t.Errorf("expected 0 detections for empty text, got %d", len(detections))
	}
}

// --- Layer 1: Keyword-anchored tests ---

func TestSecretScanner_KeywordPassword(t *testing.T) {
	s := NewSecretScanner()
	tests := []struct {
		input    string
		wantVal  string
		wantFind bool
	}{
		{"password=hunter2", "hunter2", true},
		{"password: myP@ssw0rd!", "myP@ssw0rd!", true},
		{"PASSWORD = SuperSecret123", "SuperSecret123", true},
		{`secret="sk-abcdef123456"`, "sk-abcdef123456", true},
		{`token: ghp_xxxxxxxxxxxxxxxxxxxx`, "ghp_xxxxxxxxxxxxxxxxxxxx", true},
		{"api_key=AKIAIOSFODNN7EXAMPLE", "AKIAIOSFODNN7EXAMPLE", true},
		{"API-KEY: some-long-key-value", "some-long-key-value", true},
		{"access_key=AKIA1234567890ABCDEF", "AKIA1234567890ABCDEF", true},
		{"client_secret: very_secret_value_here", "very_secret_value_here", true},
	}

	for _, tt := range tests {
		detections := s.Scan(tt.input)
		found := false
		for _, d := range detections {
			if d.Layer == "keyword" && d.Original == tt.wantVal {
				found = true
				break
			}
		}
		if found != tt.wantFind {
			t.Errorf("Scan(%q): expected keyword detection of %q=%v, got detections=%v",
				tt.input, tt.wantVal, tt.wantFind, detections)
		}
	}
}

func TestSecretScanner_KeywordSkipsShortValues(t *testing.T) {
	s := NewSecretScanner()
	// Values shorter than 4 chars should not be flagged
	detections := s.Scan("password=abc")
	for _, d := range detections {
		if d.Layer == "keyword" {
			t.Errorf("expected no keyword detection for short value, got %v", d)
		}
	}
}

func TestSecretScanner_KeywordSkipsVariableRefs(t *testing.T) {
	s := NewSecretScanner()
	// Variable references like {password}, ${TOKEN} should not be flagged
	tests := []string{
		"password={user_password}",
		"token=${MY_TOKEN}",
		"secret=$SECRET_VAR",
	}
	for _, input := range tests {
		detections := s.Scan(input)
		for _, d := range detections {
			if d.Layer == "keyword" {
				t.Errorf("Scan(%q): should skip variable reference, got detection %v", input, d)
			}
		}
	}
}

func TestSecretScanner_KeywordSkipsPlaceholders(t *testing.T) {
	s := NewSecretScanner()
	tests := []string{
		"password=<your_password>",
		"token=<<<SECRET_1>>>",
		"secret={{CREDENTIAL:my-cred:password}}",
	}
	for _, input := range tests {
		detections := s.Scan(input)
		for _, d := range detections {
			if d.Layer == "keyword" {
				t.Errorf("Scan(%q): should skip placeholder, got detection %v", input, d)
			}
		}
	}
}

// --- Layer 2: Entropy tests ---

func TestSecretScanner_EntropyHighRandomness(t *testing.T) {
	s := NewSecretScanner()
	// A random-looking string with high entropy
	highEntropy := "aB3$dE7fG9!hJ2kL"
	detections := s.Scan("Here is the key: " + highEntropy)

	found := false
	for _, d := range detections {
		if d.Layer == "entropy" && d.Original == highEntropy {
			found = true
		}
	}
	if !found {
		t.Errorf("expected entropy detection of %q, got %v", highEntropy, detections)
	}
}

func TestSecretScanner_EntropySkipsNormalText(t *testing.T) {
	s := NewSecretScanner()
	// Normal English words should not trigger entropy detection
	detections := s.Scan("The quick brown fox jumps over the lazy dog")
	for _, d := range detections {
		if d.Layer == "entropy" {
			t.Errorf("normal text should not trigger entropy detection: %v", d)
		}
	}
}

func TestSecretScanner_EntropySkipsShortTokens(t *testing.T) {
	s := NewSecretScanner()
	// Short random-looking strings should be skipped
	detections := s.Scan("abc123!@# xyz")
	for _, d := range detections {
		if d.Layer == "entropy" {
			t.Errorf("short tokens should not trigger entropy: %v", d)
		}
	}
}

// --- Layer 3: Structural tests ---

func TestSecretScanner_StructuralHighDiversity(t *testing.T) {
	s := NewSecretScanner()
	// A string with 3+ char classes but below entropy threshold
	// (repeating pattern with diverse characters)
	diverse := "Aa1!Bb2@Cc3#Dd4$Ee5%Ff"
	detections := s.Scan(diverse)

	found := false
	for _, d := range detections {
		if (d.Layer == "structural" || d.Layer == "entropy") && d.Original == diverse {
			found = true
		}
	}
	if !found {
		t.Errorf("expected structural/entropy detection of diverse string %q, got %v", diverse, detections)
	}
}

// --- Safe pattern exclusion tests ---

func TestSecretScanner_SafePatternsNotFlagged(t *testing.T) {
	s := NewSecretScanner()

	safeStrings := []string{
		// UUID
		"550e8400-e29b-41d4-a716-446655440000",
		// URL without credentials
		"https://identity-3.qa-de-1.cloud.sap/v3",
		// File path
		"/usr/local/bin/openstack-cli-tool",
		// Email
		"admin@mycompany.example.com",
		// SHA-256 hex hash
		"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		// SHA-1 hex hash (git commit)
		"da39a3ee5e6b4b0d3255bfef95601890afd80709",
		// Docker digest
		"sha256:e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		// Semantic version
		"v1.24.4-beta.1+build.123",
	}

	for _, safe := range safeStrings {
		detections := s.Scan(safe)
		for _, d := range detections {
			t.Errorf("safe pattern %q should not be flagged, got detection: layer=%s", safe, d.Layer)
		}
	}
}

func TestSecretScanner_AlreadyWrappedNotFlagged(t *testing.T) {
	s := NewSecretScanner()
	// Already-wrapped secrets should not be flagged
	tests := []string{
		"<<<SECRET_1>>>",
		"{{CREDENTIAL:my-cred:password}}",
	}
	for _, input := range tests {
		detections := s.Scan(input)
		if len(detections) > 0 {
			t.Errorf("already-wrapped %q should not be flagged, got %v", input, detections)
		}
	}
}

// --- Shannon entropy unit tests ---

func TestShannonEntropy(t *testing.T) {
	tests := []struct {
		input   string
		minBits float64
		maxBits float64
	}{
		{"aaaaaaa", 0.0, 0.1},                      // single char = 0 entropy
		{"abcdefg", 2.8, 2.9},                      // 7 unique chars
		{"aB3$dE7fG9!hJ2kL", 4.0, 5.0},             // high entropy mixed
		{"password", 2.5, 3.5},                     // low-entropy English word
		{"correct_horse_battery_staple", 3.0, 4.0}, // passphrase, moderate
	}

	for _, tt := range tests {
		entropy := shannonEntropy(tt.input)
		if entropy < tt.minBits || entropy > tt.maxBits {
			t.Errorf("shannonEntropy(%q) = %.3f, expected [%.1f, %.1f]",
				tt.input, entropy, tt.minBits, tt.maxBits)
		}
	}
}

// --- Integration test: full message with mixed content ---

func TestSecretScanner_MixedMessage(t *testing.T) {
	s := NewSecretScanner()
	msg := `Here are my OpenStack credentials:
OS_AUTH_URL=https://identity-3.qa-de-1.cloud.sap/v3
OS_REGION_NAME=qa-de-1
password=xK9#mP2$vL5nQ8wR
app_credential_id=3f11fe04abbf42ec9a304105d0a800dc
token: ghp_vR8xKp3mNq2wLz5yBt7jDf4hSc6aEg9UiOlA`

	detections := s.Scan(msg)

	// Should detect the password value
	foundPassword := false
	foundToken := false
	for _, d := range detections {
		if strings.Contains(d.Original, "xK9#mP2$vL5nQ8wR") {
			foundPassword = true
		}
		if strings.Contains(d.Original, "ghp_vR8xKp3mNq2wLz5yBt7jDf4hSc6aEg9UiOlA") {
			foundToken = true
		}
	}
	if !foundPassword {
		t.Error("expected detection of password value")
	}
	if !foundToken {
		t.Error("expected detection of token value")
	}

	// Should NOT detect the URL (safe pattern)
	for _, d := range detections {
		if strings.Contains(d.Original, "https://identity") {
			t.Errorf("URL should not be detected as secret: %v", d)
		}
	}

	// Should NOT detect the UUID-like credential ID (it's a hex hash of known length)
	for _, d := range detections {
		if d.Original == "3f11fe04abbf42ec9a304105d0a800dc" {
			t.Errorf("MD5-length hex string should not be detected: %v", d)
		}
	}
}

// --- isHighDiversity tests ---

func TestIsHighDiversity(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"abcdefghijklmnopqrst", false}, // only lowercase
		{"ABCDEFGHIJKLMNOPQRST", false}, // only uppercase
		{"12345678901234567890", false}, // only digits
		{"aBcDeFgHiJkLmNoPqRsT", false}, // upper + lower only = 2 classes
		{"aB1cD2eF3gH4iJ5kL6mN", true},  // upper + lower + digits = 3
		{"aB1!cD2@eF3#gH4$iJ5%", true},  // all 4 classes
	}

	for _, tt := range tests {
		got := isHighDiversity(tt.input)
		if got != tt.want {
			t.Errorf("isHighDiversity(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// --- Tokenizer tests ---

func TestTokenize(t *testing.T) {
	tokens := tokenize(`hello "world" 'test' foo`)
	if len(tokens) != 4 {
		t.Fatalf("expected 4 tokens, got %d: %v", len(tokens), tokens)
	}
	// Check that quotes are stripped
	if tokens[1].value != "world" {
		t.Errorf("expected 'world', got %q", tokens[1].value)
	}
	if tokens[2].value != "test" {
		t.Errorf("expected 'test', got %q", tokens[2].value)
	}
}
