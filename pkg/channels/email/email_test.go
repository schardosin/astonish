package email

import (
	"log"
	"testing"
	"time"
)

func TestExtractEmailAddr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"standard format", "John Doe <john@example.com>", "john@example.com"},
		{"bare address", "john@example.com", "john@example.com"},
		{"angle brackets only", "<john@example.com>", "john@example.com"},
		{"case normalization", "John <JOHN@Example.COM>", "john@example.com"},
		{"with quotes in name", `"Doe, John" <john@example.com>`, "john@example.com"},
		{"bare uppercase", "UPPER@EXAMPLE.COM", "upper@example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractEmailAddr(tt.input)
			if got != tt.want {
				t.Errorf("extractEmailAddr(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestExtractName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"standard format", "John Doe <john@example.com>", "John Doe"},
		{"no name returns full string", "john@example.com", "john@example.com"},
		{"quoted name", `"Doe, John" <john@example.com>`, "Doe, John"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractName(tt.input)
			if got != tt.want {
				t.Errorf("extractName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsAllowed(t *testing.T) {
	t.Parallel()
	t.Run("allowAll permits anyone", func(t *testing.T) {
		t.Parallel()
		ch := &EmailChannel{
			allowAll: true,
			allowSet: make(map[string]bool),
		}
		if !ch.isAllowed("random@example.com") {
			t.Error("allowAll=true should permit any address")
		}
	})

	t.Run("empty allowSet blocks everyone", func(t *testing.T) {
		t.Parallel()
		ch := &EmailChannel{
			allowAll: false,
			allowSet: make(map[string]bool),
		}
		if ch.isAllowed("someone@example.com") {
			t.Error("empty allowSet should block all addresses")
		}
	})

	t.Run("case insensitive matching", func(t *testing.T) {
		t.Parallel()
		ch := &EmailChannel{
			allowAll: false,
			allowSet: map[string]bool{
				"alice@example.com": true,
			},
		}
		if !ch.isAllowed("Alice@Example.COM") {
			t.Error("isAllowed should be case-insensitive")
		}
		if ch.isAllowed("bob@example.com") {
			t.Error("isAllowed should reject addresses not in allowSet")
		}
	})
}

func TestNewDefaults(t *testing.T) {
	t.Parallel()
	logger := log.Default()

	t.Run("PollInterval defaults to 30s", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Address: "bot@example.com"}
		ch := New(cfg, logger)
		if ch.config.PollInterval != 30*time.Second {
			t.Errorf("PollInterval = %v, want 30s", ch.config.PollInterval)
		}
	})

	t.Run("Folder defaults to INBOX", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Address: "bot@example.com"}
		ch := New(cfg, logger)
		if ch.config.Folder != "INBOX" {
			t.Errorf("Folder = %q, want %q", ch.config.Folder, "INBOX")
		}
	})

	t.Run("MaxBodyChars defaults to 50000", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Address: "bot@example.com"}
		ch := New(cfg, logger)
		if ch.config.MaxBodyChars != 50000 {
			t.Errorf("MaxBodyChars = %d, want 50000", ch.config.MaxBodyChars)
		}
	})

	t.Run("custom values preserved", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:      "bot@example.com",
			PollInterval: 60 * time.Second,
			Folder:       "Custom",
			MaxBodyChars: 1000,
		}
		ch := New(cfg, logger)
		if ch.config.PollInterval != 60*time.Second {
			t.Errorf("PollInterval = %v, want 60s", ch.config.PollInterval)
		}
		if ch.config.Folder != "Custom" {
			t.Errorf("Folder = %q, want %q", ch.config.Folder, "Custom")
		}
		if ch.config.MaxBodyChars != 1000 {
			t.Errorf("MaxBodyChars = %d, want 1000", ch.config.MaxBodyChars)
		}
	})

	t.Run("AllowFrom with star sets allowAll", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:   "bot@example.com",
			AllowFrom: []string{"*"},
		}
		ch := New(cfg, logger)
		if !ch.allowAll {
			t.Error("AllowFrom with '*' should set allowAll to true")
		}
	})

	t.Run("AllowFrom with addresses populates allowSet", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:   "bot@example.com",
			AllowFrom: []string{"Alice@Example.com", "bob@test.org"},
		}
		ch := New(cfg, logger)
		if ch.allowAll {
			t.Error("allowAll should be false for specific addresses")
		}
		// Addresses should be lowercased in the set
		if !ch.allowSet["alice@example.com"] {
			t.Error("allowSet should contain lowercased alice@example.com")
		}
		if !ch.allowSet["bob@test.org"] {
			t.Error("allowSet should contain lowercased bob@test.org")
		}
	})

	t.Run("nil logger uses default", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Address: "bot@example.com"}
		ch := New(cfg, nil)
		if ch.logger == nil {
			t.Error("logger should not be nil when nil is passed")
		}
	})
}
