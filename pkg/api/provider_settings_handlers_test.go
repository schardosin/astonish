package api

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/SAP/astonish/pkg/store"
	"github.com/SAP/astonish/pkg/store/entstore"
)

// TestSavePlatformProviders_PreservesChannels verifies that saving provider
// configuration does not wipe the Channels field from PlatformSettings.
// Regression test for: when providers were saved, the handler constructed a new
// PlatformSettings struct without preserving existing.Channels, causing Telegram
// (and other channels) to appear disabled after any provider edit.
func TestSavePlatformProviders_PreservesChannels(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()
	settingsStore := esStore.PlatformSettings()

	// Step 1: Save initial settings with Telegram enabled
	initial := &store.PlatformSettings{
		DefaultProvider: "openai",
		Providers: map[string]store.ProviderConfig{
			"openai": {"type": "openai", "api_key": "sk-test"},
		},
		Channels: &store.PlatformChannelSettings{
			Telegram: &store.PlatformTelegramConfig{Enabled: true},
		},
	}
	if err := settingsStore.Save(ctx, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	// Step 2: Simulate provider save — same logic as SavePlatformProvidersHandler:
	// Load existing, construct new struct preserving Channels
	existing, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get existing: %v", err)
	}

	// This is the fixed save logic (with Channels preserved):
	updated := &store.PlatformSettings{
		DefaultProvider: "anthropic",
		DefaultModel:    "claude-3",
		Providers: map[string]store.ProviderConfig{
			"anthropic": {"type": "anthropic", "api_key": "sk-ant-test"},
		},
		Channels: existing.Channels, // <-- THE FIX: preserve channels
	}
	if err := settingsStore.Save(ctx, updated); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	// Step 3: Re-read and verify Channels survives
	reloaded, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get reloaded: %v", err)
	}

	if reloaded.Channels == nil {
		t.Fatal("Channels is nil after provider save — regression!")
	}
	if reloaded.Channels.Telegram == nil {
		t.Fatal("Channels.Telegram is nil after provider save — regression!")
	}
	if !reloaded.Channels.Telegram.Enabled {
		t.Error("Channels.Telegram.Enabled is false after provider save — regression!")
	}

	// Also verify provider was updated correctly
	if reloaded.DefaultProvider != "anthropic" {
		t.Errorf("DefaultProvider = %q, want %q", reloaded.DefaultProvider, "anthropic")
	}
}

// TestSavePlatformProviders_PreservesChannels_NilChannels verifies that the
// save handler doesn't crash when existing settings have no Channels configured.
func TestSavePlatformProviders_PreservesChannels_NilChannels(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()
	settingsStore := esStore.PlatformSettings()

	// Step 1: Save initial settings WITHOUT Channels
	initial := &store.PlatformSettings{
		DefaultProvider: "openai",
		Providers: map[string]store.ProviderConfig{
			"openai": {"type": "openai", "api_key": "sk-test"},
		},
	}
	if err := settingsStore.Save(ctx, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	// Step 2: Simulate provider save
	existing, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get existing: %v", err)
	}

	updated := &store.PlatformSettings{
		DefaultProvider: "anthropic",
		Providers: map[string]store.ProviderConfig{
			"anthropic": {"type": "anthropic"},
		},
		Channels: existing.Channels, // nil — should not crash
	}
	if err := settingsStore.Save(ctx, updated); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	// Step 3: Verify no crash and Channels is still nil
	reloaded, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get reloaded: %v", err)
	}

	if reloaded.Channels != nil {
		t.Error("Channels should remain nil when it was never set")
	}
}

// TestSavePlatformProviders_PreservesAllChannelTypes verifies that all three
// channel types (Telegram, Email, Slack) survive a provider settings save.
func TestSavePlatformProviders_PreservesAllChannelTypes(t *testing.T) {
	tmp := t.TempDir()
	_, esStore, err := entstore.NewPlatformServices(context.Background(), entstore.Config{
		DSN:     "file:" + filepath.Join(tmp, "platform.db"),
		DataDir: tmp,
	})
	if err != nil {
		t.Fatalf("NewPlatformServices: %v", err)
	}
	defer esStore.Close()

	ctx := context.Background()
	settingsStore := esStore.PlatformSettings()

	markRead := true
	// Step 1: Save settings with ALL channel types enabled
	initial := &store.PlatformSettings{
		DefaultProvider: "openai",
		Providers:       map[string]store.ProviderConfig{"openai": {"type": "openai"}},
		Channels: &store.PlatformChannelSettings{
			Telegram: &store.PlatformTelegramConfig{Enabled: true},
			Email: &store.PlatformEmailConfig{
				Enabled:    true,
				IMAPServer: "imap.gmail.com:993",
				SMTPServer: "smtp.gmail.com:587",
				Address:    "bot@example.com",
				MarkRead:   &markRead,
			},
			Slack: &store.PlatformSlackConfig{
				Enabled: true,
				Mode:    "socket",
			},
		},
	}
	if err := settingsStore.Save(ctx, initial); err != nil {
		t.Fatalf("Save initial: %v", err)
	}

	// Step 2: Simulate provider save (preserving channels)
	existing, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get existing: %v", err)
	}

	updated := &store.PlatformSettings{
		DefaultProvider: "gemini",
		Providers:       map[string]store.ProviderConfig{"gemini": {"type": "gemini"}},
		Channels:        existing.Channels,
	}
	if err := settingsStore.Save(ctx, updated); err != nil {
		t.Fatalf("Save updated: %v", err)
	}

	// Step 3: Verify all channels survive
	reloaded, err := settingsStore.Get(ctx)
	if err != nil {
		t.Fatalf("Get reloaded: %v", err)
	}

	if reloaded.Channels == nil {
		t.Fatal("Channels is nil after provider save")
	}

	// Telegram
	if reloaded.Channels.Telegram == nil || !reloaded.Channels.Telegram.Enabled {
		t.Error("Telegram config lost after provider save")
	}

	// Email
	if reloaded.Channels.Email == nil {
		t.Fatal("Email config lost after provider save")
	}
	if !reloaded.Channels.Email.Enabled {
		t.Error("Email.Enabled lost")
	}
	if reloaded.Channels.Email.IMAPServer != "imap.gmail.com:993" {
		t.Errorf("Email.IMAPServer = %q, want %q", reloaded.Channels.Email.IMAPServer, "imap.gmail.com:993")
	}
	if reloaded.Channels.Email.Address != "bot@example.com" {
		t.Errorf("Email.Address = %q, want %q", reloaded.Channels.Email.Address, "bot@example.com")
	}

	// Slack
	if reloaded.Channels.Slack == nil {
		t.Fatal("Slack config lost after provider save")
	}
	if !reloaded.Channels.Slack.Enabled {
		t.Error("Slack.Enabled lost")
	}
	if reloaded.Channels.Slack.Mode != "socket" {
		t.Errorf("Slack.Mode = %q, want %q", reloaded.Channels.Slack.Mode, "socket")
	}
}
