package daemon

import (
	"testing"

	"github.com/schardosin/astonish/pkg/config"
)

func boolPtr(b bool) *bool { return &b }

func TestNeedsFullReload(t *testing.T) {
	t.Parallel()

	base := func() *config.AppConfig {
		return &config.AppConfig{
			Channels: config.ChannelsConfig{
				Enabled: boolPtr(true),
				Telegram: config.TelegramConfig{
					Enabled:   boolPtr(true),
					AllowFrom: []string{"123"},
				},
				Email: config.EmailConfig{
					Enabled:      boolPtr(true),
					Provider:     "imap",
					IMAPServer:   "imap.gmail.com:993",
					SMTPServer:   "smtp.gmail.com:587",
					Address:      "bot@example.com",
					Username:     "bot@example.com",
					PollInterval: 30,
					Folder:       "INBOX",
					MarkRead:     boolPtr(true),
					MaxBodyChars: 50000,
					AllowFrom:    []string{"user@example.com"},
				},
			},
		}
	}

	t.Run("no changes", func(t *testing.T) {
		t.Parallel()
		old := takeSnapshot(base())
		new := takeSnapshot(base())
		if needsFullReload(old, new) {
			t.Error("identical configs should not need full reload")
		}
	})

	t.Run("allowlist only change does not need full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Email.AllowFrom = []string{"user@example.com", "new@example.com"}
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if needsFullReload(old, new) {
			t.Error("allowlist-only change should not need full reload")
		}
	})

	t.Run("telegram allowlist only change does not need full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Telegram.AllowFrom = []string{"123", "456"}
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if needsFullReload(old, new) {
			t.Error("telegram allowlist-only change should not need full reload")
		}
	})

	t.Run("IMAP server change needs full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Email.IMAPServer = "imap.other.com:993"
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if !needsFullReload(old, new) {
			t.Error("IMAP server change should need full reload")
		}
	})

	t.Run("SMTP server change needs full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Email.SMTPServer = "smtp.other.com:587"
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if !needsFullReload(old, new) {
			t.Error("SMTP server change should need full reload")
		}
	})

	t.Run("email enabled change needs full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Email.Enabled = boolPtr(false)
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if !needsFullReload(old, new) {
			t.Error("email enabled change should need full reload")
		}
	})

	t.Run("channels enabled change needs full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Enabled = boolPtr(false)
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if !needsFullReload(old, new) {
			t.Error("channels enabled change should need full reload")
		}
	})

	t.Run("poll interval change needs full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Email.PollInterval = 60
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if !needsFullReload(old, new) {
			t.Error("poll interval change should need full reload")
		}
	})

	t.Run("telegram enabled change needs full reload", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Telegram.Enabled = boolPtr(false)
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if !needsFullReload(old, new) {
			t.Error("telegram enabled change should need full reload")
		}
	})
}

func TestAllowlistChanged(t *testing.T) {
	t.Parallel()

	base := func() *config.AppConfig {
		return &config.AppConfig{
			Channels: config.ChannelsConfig{
				Enabled: boolPtr(true),
				Telegram: config.TelegramConfig{
					Enabled:   boolPtr(true),
					AllowFrom: []string{"123"},
				},
				Email: config.EmailConfig{
					Enabled:   boolPtr(true),
					AllowFrom: []string{"user@example.com"},
				},
			},
		}
	}

	t.Run("no changes returns nil", func(t *testing.T) {
		t.Parallel()
		old := takeSnapshot(base())
		new := takeSnapshot(base())
		if changes := allowlistChanged(old, new); changes != nil {
			t.Errorf("identical allowlists should return nil, got %v", changes)
		}
	})

	t.Run("email allowlist added", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Email.AllowFrom = []string{"user@example.com", "new@sap.com"}
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		changes := allowlistChanged(old, new)
		if changes == nil {
			t.Fatal("should detect email allowlist change")
		}
		if _, has := changes["email"]; !has {
			t.Error("changes should contain 'email' key")
		}
		if _, has := changes["telegram"]; has {
			t.Error("changes should not contain 'telegram' key (unchanged)")
		}
	})

	t.Run("telegram allowlist changed", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Telegram.AllowFrom = []string{"123", "456"}
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		changes := allowlistChanged(old, new)
		if changes == nil {
			t.Fatal("should detect telegram allowlist change")
		}
		if _, has := changes["telegram"]; !has {
			t.Error("changes should contain 'telegram' key")
		}
		if _, has := changes["email"]; has {
			t.Error("changes should not contain 'email' key (unchanged)")
		}
	})

	t.Run("both allowlists changed", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg2 := base()
		cfg2.Channels.Email.AllowFrom = []string{"new@example.com"}
		cfg2.Channels.Telegram.AllowFrom = []string{"789"}
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		changes := allowlistChanged(old, new)
		if changes == nil {
			t.Fatal("should detect both allowlist changes")
		}
		if _, has := changes["email"]; !has {
			t.Error("changes should contain 'email' key")
		}
		if _, has := changes["telegram"]; !has {
			t.Error("changes should contain 'telegram' key")
		}
	})

	t.Run("order independent comparison", func(t *testing.T) {
		t.Parallel()
		cfg1 := base()
		cfg1.Channels.Email.AllowFrom = []string{"a@example.com", "b@example.com"}
		cfg2 := base()
		cfg2.Channels.Email.AllowFrom = []string{"b@example.com", "a@example.com"}
		old := takeSnapshot(cfg1)
		new := takeSnapshot(cfg2)
		if changes := allowlistChanged(old, new); changes != nil {
			t.Error("same entries in different order should not count as changed")
		}
	})
}

func TestSlicesEqual(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a, b []string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"both empty", []string{}, []string{}, true},
		{"nil vs empty", nil, []string{}, true},
		{"same elements same order", []string{"a", "b"}, []string{"a", "b"}, true},
		{"same elements different order", []string{"b", "a"}, []string{"a", "b"}, true},
		{"different lengths", []string{"a"}, []string{"a", "b"}, false},
		{"different elements", []string{"a", "b"}, []string{"a", "c"}, false},
		{"single element match", []string{"x"}, []string{"x"}, true},
		{"single element mismatch", []string{"x"}, []string{"y"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := slicesEqual(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("slicesEqual(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
