package channels

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/schardosin/astonish/pkg/provider/llmerror"
)

func TestTruncate(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"short unchanged", "hello", 10, "hello"},
		{"exact length", "hello", 5, "hello"},
		{"truncation adds ellipsis", "hello world", 8, "hello..."},
		{"empty string", "", 10, ""},
		{"truncate to minimum", "abcdef", 4, "a..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := truncate(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestChannelHints(t *testing.T) {
	t.Parallel()
	t.Run("telegram returns non-empty", func(t *testing.T) {
		t.Parallel()
		hints := channelHints("telegram")
		if hints == "" {
			t.Error("channelHints(\"telegram\") should return non-empty string")
		}
		if !strings.Contains(hints, "Telegram") {
			t.Error("telegram hints should mention Telegram")
		}
	})

	t.Run("email returns non-empty", func(t *testing.T) {
		t.Parallel()
		hints := channelHints("email")
		if hints == "" {
			t.Error("channelHints(\"email\") should return non-empty string")
		}
		if !strings.Contains(hints, "email") {
			t.Error("email hints should mention email")
		}
	})

	t.Run("unknown returns empty", func(t *testing.T) {
		t.Parallel()
		hints := channelHints("unknown_channel")
		if hints != "" {
			t.Errorf("channelHints(\"unknown_channel\") = %q, want empty", hints)
		}
	})

	t.Run("empty returns empty", func(t *testing.T) {
		t.Parallel()
		hints := channelHints("")
		if hints != "" {
			t.Errorf("channelHints(\"\") = %q, want empty", hints)
		}
	})
}

func TestFriendlyErrorMessage(t *testing.T) {
	t.Parallel()
	t.Run("rate limited error", func(t *testing.T) {
		t.Parallel()
		err := llmerror.NewLLMError("openai", 429, "rate limited", "")
		msg := friendlyErrorMessage(err)
		if !strings.Contains(msg, "rate limited") {
			t.Errorf("expected rate limit message, got %q", msg)
		}
	})

	t.Run("auth error", func(t *testing.T) {
		t.Parallel()
		err := llmerror.NewLLMError("openai", 401, "unauthorized", "")
		msg := friendlyErrorMessage(err)
		if !strings.Contains(msg, "Authentication") {
			t.Errorf("expected auth error message, got %q", msg)
		}
	})

	t.Run("server error", func(t *testing.T) {
		t.Parallel()
		err := llmerror.NewLLMError("openai", 500, "server error", "")
		msg := friendlyErrorMessage(err)
		if !strings.Contains(msg, "provider is experiencing issues") {
			t.Errorf("expected server error message, got %q", msg)
		}
	})

	t.Run("other status code", func(t *testing.T) {
		t.Parallel()
		err := llmerror.NewLLMError("openai", 400, "bad request", "")
		msg := friendlyErrorMessage(err)
		if !strings.Contains(msg, "HTTP 400") {
			t.Errorf("expected HTTP status code in message, got %q", msg)
		}
	})

	t.Run("plain error", func(t *testing.T) {
		t.Parallel()
		err := errors.New("something broke")
		msg := friendlyErrorMessage(err)
		if !strings.Contains(msg, "something broke") {
			t.Errorf("expected error text in message, got %q", msg)
		}
		if !strings.Contains(msg, "Sorry") {
			t.Errorf("expected 'Sorry' prefix, got %q", msg)
		}
	})

	t.Run("long error message is truncated", func(t *testing.T) {
		t.Parallel()
		longMsg := strings.Repeat("x", 300)
		err := fmt.Errorf("%s", longMsg)
		msg := friendlyErrorMessage(err)
		if len(msg) > 300 {
			// The output should contain the truncated error message (200 chars + "...")
			if !strings.Contains(msg, "...") {
				t.Errorf("expected truncation ellipsis in long error message")
			}
		}
	})
}

func TestFleetSessionTracking(t *testing.T) {
	t.Parallel()
	logger := log.Default()
	m := &ChannelManager{
		channels:        make(map[string]Channel),
		activeFleets:    make(map[string]string),
		pendingContexts: make(map[string]string),
		logger:          logger,
	}

	t.Run("get returns empty for unset key", func(t *testing.T) {
		got := m.GetActiveFleet("nonexistent")
		if got != "" {
			t.Errorf("GetActiveFleet for unset key = %q, want empty", got)
		}
	})

	t.Run("set and get", func(t *testing.T) {
		m.SetActiveFleet("chat1", "fleet-session-123")
		got := m.GetActiveFleet("chat1")
		if got != "fleet-session-123" {
			t.Errorf("GetActiveFleet = %q, want %q", got, "fleet-session-123")
		}
	})

	t.Run("clear removes mapping", func(t *testing.T) {
		m.SetActiveFleet("chat2", "fleet-session-456")
		m.ClearActiveFleet("chat2")
		got := m.GetActiveFleet("chat2")
		if got != "" {
			t.Errorf("GetActiveFleet after Clear = %q, want empty", got)
		}
	})

	t.Run("clear non-existent is safe", func(t *testing.T) {
		m.ClearActiveFleet("never-set")
		// Should not panic
	})

	t.Run("overwrite existing", func(t *testing.T) {
		m.SetActiveFleet("chat3", "first")
		m.SetActiveFleet("chat3", "second")
		got := m.GetActiveFleet("chat3")
		if got != "second" {
			t.Errorf("GetActiveFleet after overwrite = %q, want %q", got, "second")
		}
	})
}

func TestSessionContext(t *testing.T) {
	t.Parallel()
	logger := log.Default()
	m := &ChannelManager{
		channels:        make(map[string]Channel),
		activeFleets:    make(map[string]string),
		pendingContexts: make(map[string]string),
		logger:          logger,
	}

	t.Run("set then consume", func(t *testing.T) {
		m.SetSessionContext("sess1", "wizard prompt here")
		got := m.consumeSessionContext("sess1")
		if got != "wizard prompt here" {
			t.Errorf("consumeSessionContext = %q, want %q", got, "wizard prompt here")
		}
	})

	t.Run("double consume returns empty", func(t *testing.T) {
		m.SetSessionContext("sess2", "one-shot context")
		_ = m.consumeSessionContext("sess2")
		got := m.consumeSessionContext("sess2")
		if got != "" {
			t.Errorf("second consumeSessionContext = %q, want empty", got)
		}
	})

	t.Run("consume unset key returns empty", func(t *testing.T) {
		got := m.consumeSessionContext("never-set")
		if got != "" {
			t.Errorf("consumeSessionContext for unset key = %q, want empty", got)
		}
	})

	t.Run("overwrite before consume", func(t *testing.T) {
		m.SetSessionContext("sess3", "first")
		m.SetSessionContext("sess3", "second")
		got := m.consumeSessionContext("sess3")
		if got != "second" {
			t.Errorf("consumeSessionContext after overwrite = %q, want %q", got, "second")
		}
	})
}
