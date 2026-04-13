package daemon

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/schardosin/astonish/pkg/channels"
	"github.com/schardosin/astonish/pkg/config"
)

// ConfigWatcherOpts holds the options for the config file watcher.
type ConfigWatcherOpts struct {
	// DebounceMs is the debounce window in milliseconds. Default: 1500.
	DebounceMs int
	// Logger for watcher messages.
	Logger *Logger
	// GetManager returns the current ChannelManager (may change on full reload).
	GetManager func() *channels.ChannelManager
	// ReloadChannels triggers a full channel reload (stop all, re-read config, start all).
	ReloadChannels func() error
	// LastConfig is the config snapshot at daemon startup, used as the baseline
	// for change detection.
	LastConfig *config.AppConfig
}

// channelSnapshot captures the channel-relevant fields from AppConfig for
// comparison. Structural fields require a full channel reload when changed,
// while AllowFrom changes can be applied in-place.
type channelSnapshot struct {
	// Top-level
	ChannelsEnabled bool

	// Email structural fields
	EmailEnabled      bool
	EmailProvider     string
	EmailIMAPServer   string
	EmailSMTPServer   string
	EmailAddress      string
	EmailUsername     string
	EmailPollInterval int
	EmailFolder       string
	EmailMarkRead     bool
	EmailMaxBodyChars int

	// Email allowlist (hot-reloadable)
	EmailAllowFrom []string

	// Telegram structural fields
	TelegramEnabled bool

	// Telegram allowlist (hot-reloadable)
	TelegramAllowFrom []string
}

// takeSnapshot extracts channel-relevant fields from an AppConfig.
func takeSnapshot(cfg *config.AppConfig) channelSnapshot {
	s := channelSnapshot{
		ChannelsEnabled:   cfg.Channels.IsChannelsEnabled(),
		EmailEnabled:      cfg.Channels.Email.IsEmailEnabled(),
		EmailProvider:     cfg.Channels.Email.Provider,
		EmailIMAPServer:   cfg.Channels.Email.IMAPServer,
		EmailSMTPServer:   cfg.Channels.Email.SMTPServer,
		EmailAddress:      cfg.Channels.Email.Address,
		EmailUsername:     cfg.Channels.Email.Username,
		EmailPollInterval: cfg.Channels.Email.PollInterval,
		EmailFolder:       cfg.Channels.Email.Folder,
		EmailMarkRead:     cfg.Channels.Email.IsMarkRead(),
		EmailMaxBodyChars: cfg.Channels.Email.MaxBodyChars,
		EmailAllowFrom:    cfg.Channels.Email.AllowFrom,
		TelegramEnabled:   cfg.Channels.Telegram.IsTelegramEnabled(),
		TelegramAllowFrom: cfg.Channels.Telegram.AllowFrom,
	}
	return s
}

// needsFullReload returns true if any structural (non-allowlist) channel
// config field has changed, requiring a full channel tear-down and rebuild.
func needsFullReload(old, new channelSnapshot) bool {
	if old.ChannelsEnabled != new.ChannelsEnabled {
		return true
	}

	// Email structural fields
	if old.EmailEnabled != new.EmailEnabled ||
		old.EmailProvider != new.EmailProvider ||
		old.EmailIMAPServer != new.EmailIMAPServer ||
		old.EmailSMTPServer != new.EmailSMTPServer ||
		old.EmailAddress != new.EmailAddress ||
		old.EmailUsername != new.EmailUsername ||
		old.EmailPollInterval != new.EmailPollInterval ||
		old.EmailFolder != new.EmailFolder ||
		old.EmailMarkRead != new.EmailMarkRead ||
		old.EmailMaxBodyChars != new.EmailMaxBodyChars {
		return true
	}

	// Telegram structural fields
	if old.TelegramEnabled != new.TelegramEnabled {
		return true
	}

	return false
}

// allowlistChanged returns a map of channel ID to new allowlist for channels
// whose AllowFrom list has changed. Returns nil if no allowlists changed.
func allowlistChanged(old, new channelSnapshot) map[string][]string {
	changes := make(map[string][]string)

	if !slicesEqual(old.EmailAllowFrom, new.EmailAllowFrom) {
		changes["email"] = new.EmailAllowFrom
	}
	if !slicesEqual(old.TelegramAllowFrom, new.TelegramAllowFrom) {
		changes["telegram"] = new.TelegramAllowFrom
	}

	if len(changes) == 0 {
		return nil
	}
	return changes
}

// slicesEqual compares two string slices for equality (order-independent).
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	aSorted := make([]string, len(a))
	bSorted := make([]string, len(b))
	copy(aSorted, a)
	copy(bSorted, b)
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			return false
		}
	}
	return true
}

// WatchConfig watches the config file for changes and triggers appropriate
// reload actions: in-place allowlist updates for allow_from changes, full
// channel reload for structural changes.
//
// This function blocks until ctx is cancelled. It follows the same
// fsnotify + debounce pattern used by the memory file watcher.
func WatchConfig(ctx context.Context, configPath string, opts ConfigWatcherOpts) error {
	if opts.DebounceMs <= 0 {
		opts.DebounceMs = 1500
	}
	logger := opts.Logger
	if logger == nil {
		return fmt.Errorf("config watcher requires a logger")
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer watcher.Close()

	if err := watcher.Add(configPath); err != nil {
		return err
	}

	lastSnapshot := takeSnapshot(opts.LastConfig)

	var debounceTimer *time.Timer
	var debounceMu sync.Mutex

	handleChange := func() {
		freshCfg, err := config.LoadAppConfig()
		if err != nil {
			logger.Printf("[config-watcher] Error reading config: %v", err)
			return
		}

		newSnapshot := takeSnapshot(freshCfg)

		if needsFullReload(lastSnapshot, newSnapshot) {
			logger.Printf("[config-watcher] Structural channel config changed, performing full reload")
			if err := opts.ReloadChannels(); err != nil {
				logger.Printf("[config-watcher] Full reload error: %v", err)
			}
			lastSnapshot = newSnapshot
			return
		}

		if changes := allowlistChanged(lastSnapshot, newSnapshot); changes != nil {
			mgr := opts.GetManager()
			if mgr != nil {
				mgr.UpdateAllowlists(changes)
				logger.Printf("[config-watcher] Allowlist(s) updated in-place")
			}
			lastSnapshot = newSnapshot
			return
		}

		// Non-channel config changed — no action needed for channels.
		lastSnapshot = newSnapshot
	}

	logger.Printf("[config-watcher] Watching %s for changes", configPath)

	for {
		select {
		case <-ctx.Done():
			debounceMu.Lock()
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceMu.Unlock()
			return nil

		case event, ok := <-watcher.Events:
			if !ok {
				return nil
			}

			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				debounceMu.Lock()
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(time.Duration(opts.DebounceMs)*time.Millisecond, handleChange)
				debounceMu.Unlock()
			}

			// Some editors (vim, etc.) perform atomic saves by renaming a
			// temp file over the original. This removes the original inode
			// from the watcher. Re-add the path to keep watching.
			if event.Has(fsnotify.Rename) || event.Has(fsnotify.Remove) {
				// Small delay to let the editor finish writing the new file.
				time.Sleep(100 * time.Millisecond)
				_ = watcher.Add(configPath)
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return nil
			}
			logger.Printf("[config-watcher] Watcher error: %v", err)
		}
	}
}
