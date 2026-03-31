// Package email implements the Email channel adapter for Astonish.
// It connects to an IMAP mailbox via polling, normalizes inbound emails,
// and delivers outbound responses via SMTP with proper email threading.
package email

import (
	"context"
	"fmt"
	"log"
	"net/mail"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schardosin/astonish/pkg/channels"
	emailpkg "github.com/schardosin/astonish/pkg/email"
)

// Config holds configuration for the Email channel adapter.
type Config struct {
	// Provider selects the implementation: "imap" or "gmail". Default: "imap".
	Provider string
	// IMAP/SMTP server addresses
	IMAPServer string
	SMTPServer string
	// Agent's email address and login credentials
	Address  string
	Username string
	Password string
	// Behavior
	PollInterval time.Duration // How often to check for new emails. Default: 30s
	AllowFrom    []string      // Allowed sender addresses (empty = block all, ["*"] = allow all)
	Folder       string        // IMAP folder to monitor. Default: "INBOX"
	MarkRead     bool          // Mark processed emails as read. Default: true
	MaxBodyChars int           // Truncate long email bodies. Default: 50000
	// Commands are slash commands for the email channel (from ChannelManager)
	Commands *channels.CommandRegistry
}

// EmailChannel implements the channels.Channel interface for email.
type EmailChannel struct {
	config   *Config
	client   emailpkg.Client
	handler  channels.MessageHandler
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	logger   *log.Logger
	mu       sync.RWMutex
	status   channels.ChannelStatus
	msgCount atomic.Int64

	// seenIDs tracks Message-IDs we've already processed to avoid duplicates.
	seenIDs  map[string]bool
	seenMu   sync.Mutex
	allowSet map[string]bool
	allowAll bool
}

// New creates a new Email channel adapter.
func New(cfg *Config, logger *log.Logger) *EmailChannel {
	if logger == nil {
		logger = log.Default()
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 30 * time.Second
	}
	if cfg.Folder == "" {
		cfg.Folder = "INBOX"
	}
	if cfg.MaxBodyChars <= 0 {
		cfg.MaxBodyChars = 50000
	}

	allowSet := make(map[string]bool, len(cfg.AllowFrom))
	allowAll := false
	for _, addr := range cfg.AllowFrom {
		if addr == "*" {
			allowAll = true
		} else {
			allowSet[strings.ToLower(addr)] = true
		}
	}

	return &EmailChannel{
		config:   cfg,
		logger:   logger,
		seenIDs:  make(map[string]bool),
		allowSet: allowSet,
		allowAll: allowAll,
	}
}

// ID returns the channel identifier.
func (e *EmailChannel) ID() string { return "email" }

// Name returns a human-readable name.
func (e *EmailChannel) Name() string { return "Email" }

// Start connects to the IMAP server and begins polling for new emails.
// It calls handler for each normalized inbound email. Blocks until ctx
// is cancelled or Stop is called.
func (e *EmailChannel) Start(ctx context.Context, handler channels.MessageHandler) error {
	// Create email client
	clientCfg := &emailpkg.Config{
		Provider:     e.config.Provider,
		IMAPServer:   e.config.IMAPServer,
		SMTPServer:   e.config.SMTPServer,
		Address:      e.config.Address,
		Username:     e.config.Username,
		Password:     e.config.Password,
		Folder:       e.config.Folder,
		MarkRead:     e.config.MarkRead,
		MaxBodyChars: e.config.MaxBodyChars,
	}

	client, err := emailpkg.NewClient(clientCfg)
	if err != nil {
		e.setError(fmt.Sprintf("failed to create email client: %v", err))
		return fmt.Errorf("email: failed to create client: %w", err)
	}

	if err := client.Connect(ctx); err != nil {
		e.setError(fmt.Sprintf("failed to connect: %v", err))
		return fmt.Errorf("email: failed to connect to IMAP: %w", err)
	}

	e.mu.Lock()
	e.client = client
	e.handler = handler
	e.status = channels.ChannelStatus{
		Connected:   true,
		AccountID:   e.config.Address,
		ConnectedAt: time.Now(),
	}
	e.mu.Unlock()

	e.logger.Printf("[email] Connected as %s (IMAP: %s)", e.config.Address, e.config.IMAPServer)

	// Create cancellable context for polling
	pollCtx, cancel := context.WithCancel(ctx) //nolint:gosec // G118: cancel is stored in e.cancel and called by Stop()
	e.cancel = cancel

	// Initial check
	e.checkNewEmails(pollCtx)

	// Poll loop
	e.wg.Add(1)
	defer e.wg.Done()

	ticker := time.NewTicker(e.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			e.logger.Printf("[email] Polling stopped")
			return nil
		case <-ticker.C:
			e.checkNewEmails(pollCtx)
		}
	}
}

// Stop gracefully shuts down the email channel.
func (e *EmailChannel) Stop(ctx context.Context) error {
	if e.cancel != nil {
		e.cancel()
	}

	// Wait for the polling goroutine to finish
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		e.logger.Printf("[email] Stop timed out waiting for poll loop")
	}

	// Close IMAP connection
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.client != nil {
		_ = e.client.Close()
		e.client = nil
	}
	e.status.Connected = false
	return nil
}

// Send delivers an outbound message via email (SMTP).
func (e *EmailChannel) Send(ctx context.Context, target channels.Target, msg channels.OutboundMessage) error {
	e.mu.RLock()
	client := e.client
	e.mu.RUnlock()

	if client == nil {
		return fmt.Errorf("email client not connected")
	}

	outgoing := emailpkg.OutgoingMessage{
		To:      []string{target.ChatID}, // ChatID is the recipient email address
		Subject: "Re: Astonish",          // Will be overridden by Reply if threading
		Body:    msg.Text,
	}

	// If this is a reply to an inbound message, use Reply for proper threading
	if msg.ReplyTo != "" {
		_, err := client.Reply(ctx, msg.ReplyTo, false, outgoing)
		return err
	}

	_, err := client.Send(ctx, outgoing)
	return err
}

// BroadcastTargets returns one target per allowed sender address.
func (e *EmailChannel) BroadcastTargets() []channels.Target {
	var targets []channels.Target
	for _, addr := range e.config.AllowFrom {
		if addr == "*" {
			continue
		}
		targets = append(targets, channels.Target{
			ChannelID: "email",
			ChatID:    addr,
		})
	}
	return targets
}

// SendTyping is a no-op for email (no typing indicator concept).
func (e *EmailChannel) SendTyping(ctx context.Context, target channels.Target) error {
	return nil
}

// Status returns the current connection state.
func (e *EmailChannel) Status() channels.ChannelStatus {
	e.mu.RLock()
	defer e.mu.RUnlock()
	s := e.status
	s.MessageCount = e.msgCount.Load()
	return s
}

// checkNewEmails polls the IMAP server for new unread emails and dispatches them.
func (e *EmailChannel) checkNewEmails(ctx context.Context) {
	e.mu.RLock()
	client := e.client
	handler := e.handler
	e.mu.RUnlock()

	if client == nil || handler == nil {
		return
	}

	// List unread emails
	messages, err := client.ListMessages(ctx, emailpkg.ListOpts{
		Folder: e.config.Folder,
		Unread: true,
		Limit:  50,
	})
	if err != nil {
		e.logger.Printf("[email] Error checking new emails: %v", err)
		return
	}

	for _, summary := range messages {
		// Skip already-processed messages
		e.seenMu.Lock()
		if e.seenIDs[summary.ID] {
			e.seenMu.Unlock()
			continue
		}
		e.seenIDs[summary.ID] = true
		e.seenMu.Unlock()

		// Check allowlist
		senderAddr := extractEmailAddr(summary.From)
		if !e.isAllowed(senderAddr) {
			e.logger.Printf("[email] Blocked message from %s (not in allowlist)", senderAddr)
			continue
		}

		// Read full message for the body
		fullMsg, err := client.ReadMessage(ctx, summary.ID)
		if err != nil {
			e.logger.Printf("[email] Error reading message %s: %v", summary.ID, err)
			continue
		}

		// Build normalized inbound message
		inbound := channels.InboundMessage{
			ID:         summary.ID,
			ChannelID:  "email",
			SenderID:   senderAddr,
			SenderName: extractName(summary.From),
			ChatID:     senderAddr, // Session key uses sender address
			ChatType:   channels.ChatTypeDirect,
			Text:       fullMsg.Body,
			Timestamp:  summary.Date,
			Raw:        fullMsg,
		}

		// Mark as read if configured
		if e.config.MarkRead {
			if markErr := client.MarkRead(ctx, []string{summary.ID}); markErr != nil {
				e.logger.Printf("[email] Error marking message %s as read: %v", summary.ID, markErr)
			}
		}

		e.msgCount.Add(1)

		// Dispatch to handler (ChannelManager)
		if err := handler(ctx, inbound); err != nil {
			e.logger.Printf("[email] Error handling message %s: %v", summary.ID, err)
		}
	}

	// Prune seen IDs to prevent unbounded growth (keep last 10000)
	e.seenMu.Lock()
	if len(e.seenIDs) > 10000 {
		e.seenIDs = make(map[string]bool)
	}
	e.seenMu.Unlock()
}

// isAllowed checks if a sender is in the allowlist.
func (e *EmailChannel) isAllowed(senderAddr string) bool {
	if e.allowAll {
		return true
	}
	if len(e.allowSet) == 0 {
		return false
	}
	return e.allowSet[strings.ToLower(senderAddr)]
}

// setError updates the channel status with an error message.
func (e *EmailChannel) setError(errMsg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.Error = errMsg
	e.status.Connected = false
}

// extractEmailAddr extracts the email address from a "Name <email>" format string.
func extractEmailAddr(from string) string {
	addr, err := mail.ParseAddress(from)
	if err == nil {
		return strings.ToLower(addr.Address)
	}
	// Fallback: strip angle brackets
	from = strings.TrimSpace(from)
	if idx := strings.LastIndex(from, "<"); idx >= 0 {
		end := strings.LastIndex(from, ">")
		if end > idx {
			return strings.ToLower(from[idx+1 : end])
		}
	}
	return strings.ToLower(from)
}

// extractName extracts the display name from a "Name <email>" format string.
func extractName(from string) string {
	addr, err := mail.ParseAddress(from)
	if err == nil && addr.Name != "" {
		return addr.Name
	}
	// Fallback: use the part before <
	if idx := strings.Index(from, "<"); idx > 0 {
		return strings.TrimSpace(from[:idx])
	}
	return from
}
