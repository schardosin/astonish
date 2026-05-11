package daemon

import (
	"context"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// applyPlatformChannelConfig overlays DB-stored channel settings onto the
// file-based AppConfig.Channels. In platform mode, PlatformSettings.Channels
// is the authoritative source. Fields set in the DB take precedence; fields
// not set (nil channel config) fall through to config.yaml values.
func applyPlatformChannelConfig(pgStore *pgstore.PGStore, cfg *config.AppConfig, logger *Logger) {
	settingsStore := pgStore.PlatformSettings()
	settings, err := settingsStore.Get(context.Background())
	if err != nil || settings == nil || settings.Channels == nil {
		return
	}

	ch := settings.Channels

	// Telegram: only has enabled flag (secrets come from platform_secrets)
	if ch.Telegram != nil {
		enabled := ch.Telegram.Enabled
		cfg.Channels.Telegram.Enabled = &enabled
		logger.Printf("[channels] Platform config: Telegram enabled=%v", enabled)
	}

	// Email: has many non-secret config fields
	if ch.Email != nil {
		enabled := ch.Email.Enabled
		cfg.Channels.Email.Enabled = &enabled
		if ch.Email.Provider != "" {
			cfg.Channels.Email.Provider = ch.Email.Provider
		}
		if ch.Email.IMAPServer != "" {
			cfg.Channels.Email.IMAPServer = ch.Email.IMAPServer
		}
		if ch.Email.SMTPServer != "" {
			cfg.Channels.Email.SMTPServer = ch.Email.SMTPServer
		}
		if ch.Email.Address != "" {
			cfg.Channels.Email.Address = ch.Email.Address
		}
		if ch.Email.Username != "" {
			cfg.Channels.Email.Username = ch.Email.Username
		}
		if ch.Email.PollInterval > 0 {
			cfg.Channels.Email.PollInterval = ch.Email.PollInterval
		}
		if ch.Email.Folder != "" {
			cfg.Channels.Email.Folder = ch.Email.Folder
		}
		if ch.Email.MarkRead != nil {
			cfg.Channels.Email.MarkRead = ch.Email.MarkRead
		}
		if ch.Email.MaxBodyChars > 0 {
			cfg.Channels.Email.MaxBodyChars = ch.Email.MaxBodyChars
		}
		logger.Printf("[channels] Platform config: Email enabled=%v, address=%s, imap=%s, smtp=%s",
			enabled, cfg.Channels.Email.Address, cfg.Channels.Email.IMAPServer, cfg.Channels.Email.SMTPServer)
	}

	// Slack: mode is the main non-secret config field
	if ch.Slack != nil {
		enabled := ch.Slack.Enabled
		cfg.Channels.Slack.Enabled = &enabled
		if ch.Slack.Mode != "" {
			cfg.Channels.Slack.Mode = ch.Slack.Mode
		}
		logger.Printf("[channels] Platform config: Slack enabled=%v, mode=%s", enabled, cfg.Channels.Slack.GetMode())
	}
}
