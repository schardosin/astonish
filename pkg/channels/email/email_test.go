package email

import (
	"log"
	"path/filepath"
	"testing"
	"time"

	emailpkg "github.com/schardosin/astonish/pkg/email"
	"github.com/schardosin/astonish/pkg/session"
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

func TestUpdateAllowlist(t *testing.T) {
	t.Parallel()
	logger := log.Default()

	t.Run("replaces existing allowlist", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:   "bot@example.com",
			AllowFrom: []string{"old@example.com"},
		}
		ch := New(cfg, logger)

		if !ch.isAllowed("old@example.com") {
			t.Fatal("old address should be allowed before update")
		}

		ch.UpdateAllowlist([]string{"new@example.com", "another@example.com"})

		if ch.isAllowed("old@example.com") {
			t.Error("old address should be blocked after update")
		}
		if !ch.isAllowed("new@example.com") {
			t.Error("new@example.com should be allowed after update")
		}
		if !ch.isAllowed("another@example.com") {
			t.Error("another@example.com should be allowed after update")
		}
	})

	t.Run("handles wildcard", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:   "bot@example.com",
			AllowFrom: []string{"only@example.com"},
		}
		ch := New(cfg, logger)

		ch.UpdateAllowlist([]string{"*"})

		if !ch.isAllowed("anyone@anywhere.com") {
			t.Error("wildcard should allow any address")
		}
	})

	t.Run("wildcard to specific", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:   "bot@example.com",
			AllowFrom: []string{"*"},
		}
		ch := New(cfg, logger)

		if !ch.isAllowed("random@test.com") {
			t.Fatal("wildcard should allow any address before update")
		}

		ch.UpdateAllowlist([]string{"specific@example.com"})

		if ch.isAllowed("random@test.com") {
			t.Error("random address should be blocked after switching from wildcard to specific")
		}
		if !ch.isAllowed("specific@example.com") {
			t.Error("specific address should be allowed after update")
		}
	})

	t.Run("empty list blocks all", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:   "bot@example.com",
			AllowFrom: []string{"allowed@example.com"},
		}
		ch := New(cfg, logger)

		ch.UpdateAllowlist([]string{})

		if ch.isAllowed("allowed@example.com") {
			t.Error("empty allowlist should block all addresses")
		}
	})

	t.Run("clears seenIDs", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{
			Address:   "bot@example.com",
			AllowFrom: []string{"allowed@example.com"},
		}
		ch := New(cfg, logger)

		// Simulate some seen message IDs
		ch.seenMu.Lock()
		ch.seenIDs["msg-1"] = true
		ch.seenIDs["msg-2"] = true
		ch.seenMu.Unlock()

		ch.UpdateAllowlist([]string{"allowed@example.com", "new@example.com"})

		ch.seenMu.Lock()
		seenCount := len(ch.seenIDs)
		ch.seenMu.Unlock()

		if seenCount != 0 {
			t.Errorf("seenIDs should be empty after UpdateAllowlist, got %d entries", seenCount)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		t.Parallel()
		cfg := &Config{Address: "bot@example.com"}
		ch := New(cfg, logger)

		ch.UpdateAllowlist([]string{"Alice@Example.COM"})

		if !ch.isAllowed("alice@example.com") {
			t.Error("allowlist should be case-insensitive after update")
		}
	})
}

// --- Thread-based session routing tests ---

func newTestEmailChannel(t *testing.T) (*EmailChannel, *session.ThreadIndex) {
	t.Helper()
	dir := t.TempDir()
	idx := session.NewThreadIndex(filepath.Join(dir, "thread_index.json"))
	ch := New(&Config{Address: "bot@test.com"}, log.Default())
	ch.SetThreadIndex(idx)
	return ch, idx
}

func TestResolveThreadSession_NewEmail(t *testing.T) {
	ch, idx := newTestEmailChannel(t)

	// New email (no In-Reply-To, no References) should create a new thread session
	msg := &emailpkg.Message{
		Headers: map[string]string{
			"Message-ID": "<new-thread@example.com>",
		},
	}

	sessionKey := ch.resolveThreadSession("alice@example.com", msg)
	if sessionKey == "" {
		t.Fatal("expected non-empty session key for new thread")
	}
	if sessionKey == "alice@example.com" {
		t.Error("session key should NOT be the sender address (should be thread-specific)")
	}

	// The Message-ID should now be indexed
	got, ok := idx.Lookup("<new-thread@example.com>")
	if !ok {
		t.Fatal("expected Message-ID to be indexed after resolveThreadSession")
	}
	if got != sessionKey {
		t.Errorf("indexed session key = %q, want %q", got, sessionKey)
	}
}

func TestResolveThreadSession_Reply(t *testing.T) {
	ch, idx := newTestEmailChannel(t)

	// Set up: original email created a session
	origSessionKey := "email:direct:alice@example.com-abcd1234"
	_ = idx.Associate([]string{"<original@example.com>"}, origSessionKey)

	// Reply to the original email
	msg := &emailpkg.Message{
		Headers: map[string]string{
			"Message-ID":  "<reply-1@example.com>",
			"In-Reply-To": "<original@example.com>",
		},
	}

	sessionKey := ch.resolveThreadSession("alice@example.com", msg)
	if sessionKey != origSessionKey {
		t.Errorf("reply should route to original session: got %q, want %q", sessionKey, origSessionKey)
	}

	// The reply's Message-ID should also be indexed to the same session
	got, ok := idx.Lookup("<reply-1@example.com>")
	if !ok {
		t.Fatal("reply Message-ID should be indexed")
	}
	if got != origSessionKey {
		t.Errorf("reply indexed to %q, want %q", got, origSessionKey)
	}
}

func TestResolveThreadSession_ReferencesChainFallback(t *testing.T) {
	ch, idx := newTestEmailChannel(t)

	// Set up: an old message in the thread is indexed
	origSessionKey := "email:direct:bob@example.com-feed0001"
	_ = idx.Associate([]string{"<msg-1@example.com>"}, origSessionKey)

	// A reply arrives where In-Reply-To points to an unindexed message,
	// but References includes the older indexed message
	msg := &emailpkg.Message{
		Headers: map[string]string{
			"Message-ID":  "<msg-3@example.com>",
			"In-Reply-To": "<msg-2@example.com>", // not indexed
			"References":  "<msg-1@example.com> <msg-2@example.com>",
		},
	}

	sessionKey := ch.resolveThreadSession("bob@example.com", msg)
	if sessionKey != origSessionKey {
		t.Errorf("should fall back to References chain: got %q, want %q", sessionKey, origSessionKey)
	}
}

func TestResolveThreadSession_NoThreadIndex(t *testing.T) {
	// Without thread index, should return empty (fall back to Router's ChatID)
	ch := New(&Config{Address: "bot@test.com"}, log.Default())

	msg := &emailpkg.Message{
		Headers: map[string]string{
			"Message-ID": "<test@example.com>",
		},
	}

	sessionKey := ch.resolveThreadSession("alice@example.com", msg)
	if sessionKey != "" {
		t.Errorf("without thread index, should return empty string, got %q", sessionKey)
	}
}

func TestResolveThreadSession_DifferentSendersNewThreads(t *testing.T) {
	ch, _ := newTestEmailChannel(t)

	// Two new emails from different senders should create different sessions
	msg1 := &emailpkg.Message{
		Headers: map[string]string{"Message-ID": "<alice-1@example.com>"},
	}
	msg2 := &emailpkg.Message{
		Headers: map[string]string{"Message-ID": "<bob-1@example.com>"},
	}

	session1 := ch.resolveThreadSession("alice@example.com", msg1)
	session2 := ch.resolveThreadSession("bob@example.com", msg2)

	if session1 == session2 {
		t.Error("different senders should get different sessions")
	}
}

func TestResolveThreadSession_SameSenderDifferentThreads(t *testing.T) {
	ch, _ := newTestEmailChannel(t)

	// Two new emails from the same sender should create different sessions
	msg1 := &emailpkg.Message{
		Headers: map[string]string{"Message-ID": "<alice-thread1@example.com>"},
	}
	msg2 := &emailpkg.Message{
		Headers: map[string]string{"Message-ID": "<alice-thread2@example.com>"},
	}

	session1 := ch.resolveThreadSession("alice@example.com", msg1)
	session2 := ch.resolveThreadSession("alice@example.com", msg2)

	if session1 == session2 {
		t.Error("different threads from same sender should get different sessions")
	}
}

func TestGenerateThreadID(t *testing.T) {
	// Same inputs should produce the same thread ID (deterministic)
	id1 := generateThreadID("alice@example.com", "<msg@ex.com>")
	id2 := generateThreadID("alice@example.com", "<msg@ex.com>")
	if id1 != id2 {
		t.Errorf("generateThreadID should be deterministic: %q != %q", id1, id2)
	}

	// Different Message-IDs should produce different thread IDs
	id3 := generateThreadID("alice@example.com", "<other@ex.com>")
	if id1 == id3 {
		t.Error("different Message-IDs should produce different thread IDs")
	}

	// Should contain the sender address for readability
	if !contains(id1, "alice@example.com") {
		t.Errorf("thread ID should contain sender address: %q", id1)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && findSubstring(s, substr))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestParseReferences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "single reference",
			input: "<msg1@example.com>",
			want:  []string{"<msg1@example.com>"},
		},
		{
			name:  "multiple references",
			input: "<msg1@example.com> <msg2@example.com> <msg3@example.com>",
			want:  []string{"<msg1@example.com>", "<msg2@example.com>", "<msg3@example.com>"},
		},
		{
			name:  "extra whitespace",
			input: "  <msg1@example.com>   <msg2@example.com>  ",
			want:  []string{"<msg1@example.com>", "<msg2@example.com>"},
		},
		{
			name:  "empty string",
			input: "",
			want:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseReferences(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("parseReferences(%q) = %v (len %d), want %v (len %d)", tt.input, got, len(got), tt.want, len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("parseReferences(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.want[i])
				}
			}
		})
	}
}
