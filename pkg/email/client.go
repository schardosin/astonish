// Package email provides IMAP and SMTP email client functionality for Astonish.
// It supports reading, searching, sending, and managing emails through standard
// IMAP/SMTP protocols, with verification link extraction for portal registration flows.
package email

import (
	"context"
	"time"
)

// MessageSummary is a lightweight representation of an email for list results.
type MessageSummary struct {
	ID             string    `json:"id"`
	From           string    `json:"from"`
	To             []string  `json:"to"`
	Subject        string    `json:"subject"`
	Date           time.Time `json:"date"`
	Unread         bool      `json:"unread"`
	HasAttachments bool      `json:"has_attachments"`
	MessageID      string    `json:"message_id,omitempty"`  // RFC 2822 Message-ID header
	InReplyTo      string    `json:"in_reply_to,omitempty"` // RFC 2822 In-Reply-To header
}

// Message is a fully-parsed email.
type Message struct {
	ID      string            `json:"id"`
	From    string            `json:"from"`
	To      []string          `json:"to"`
	CC      []string          `json:"cc,omitempty"`
	Subject string            `json:"subject"`
	Date    time.Time         `json:"date"`
	Body    string            `json:"body"`              // Plain text body
	HTML    string            `json:"html,omitempty"`    // Raw HTML body (truncated)
	Headers map[string]string `json:"headers,omitempty"` // Selected headers
	Links   []string          `json:"links,omitempty"`   // All URLs in the body
	Unread  bool              `json:"unread"`

	Attachments       []AttachmentInfo `json:"attachments,omitempty"`
	VerificationLinks []string         `json:"verification_links,omitempty"`
}

// AttachmentInfo describes an email attachment without including the actual data.
type AttachmentInfo struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// OutgoingMessage represents an email to be sent.
type OutgoingMessage struct {
	To      []string `json:"to"`
	CC      []string `json:"cc,omitempty"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`               // Plain text body
	HTML    string   `json:"html,omitempty"`     // Optional HTML body
	ReplyTo string   `json:"reply_to,omitempty"` // Reply-To header address
}

// ListOpts controls which messages are returned by ListMessages.
type ListOpts struct {
	Folder  string    // IMAP folder. Default: "INBOX"
	Unread  bool      // Only unread messages
	From    string    // Filter by sender (substring match)
	Subject string    // Filter by subject (substring match)
	Since   time.Time // Only messages after this time
	Limit   int       // Max results. Default: 20
}

// SearchQuery is used for more complex email searches.
type SearchQuery struct {
	Query         string // Free-text search (subject + body)
	From          string
	To            string
	Subject       string
	Since         time.Time
	Before        time.Time
	HasAttachment bool
	Folder        string // Default: "INBOX"
	Limit         int    // Default: 20
}

// Client defines the interface for email operations. Both the IMAP/SMTP
// and Gmail API implementations satisfy this interface.
type Client interface {
	// Connect establishes the connection to the email server.
	Connect(ctx context.Context) error

	// Close shuts down the connection gracefully.
	Close() error

	// IsConnected returns whether the client has an active connection.
	IsConnected() bool

	// Address returns the email address associated with this client.
	Address() string

	// ListMessages returns a list of email summaries matching the given options.
	ListMessages(ctx context.Context, opts ListOpts) ([]MessageSummary, error)

	// ReadMessage returns the full content of a specific email.
	ReadMessage(ctx context.Context, id string) (*Message, error)

	// SearchMessages searches emails with the given criteria.
	SearchMessages(ctx context.Context, query SearchQuery) ([]MessageSummary, error)

	// Send sends a new email and returns the Message-ID.
	Send(ctx context.Context, msg OutgoingMessage) (string, error)

	// Reply sends a reply to an existing email and returns the Message-ID.
	// The replyToID identifies the message being replied to.
	// If replyAll is true, the reply goes to all original recipients.
	Reply(ctx context.Context, replyToID string, replyAll bool, msg OutgoingMessage) (string, error)

	// MarkRead marks the given messages as read.
	MarkRead(ctx context.Context, ids []string) error

	// MarkUnread marks the given messages as unread.
	MarkUnread(ctx context.Context, ids []string) error

	// Delete moves messages to trash (or permanently deletes if permanent is true).
	Delete(ctx context.Context, ids []string, permanent bool) error
}

// Config holds configuration for creating an email client.
type Config struct {
	// Provider selects the implementation: "imap" or "gmail"
	Provider string

	// IMAP/SMTP settings
	IMAPServer string
	SMTPServer string
	Address    string
	Username   string
	Password   string

	// Behavior
	PollInterval time.Duration
	Folder       string // Default: "INBOX"
	MarkRead     bool   // Mark processed messages as read. Default: true
	MaxBodyChars int    // Truncate email bodies. Default: 50000
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		Provider:     "imap",
		Folder:       "INBOX",
		MarkRead:     true,
		MaxBodyChars: 50000,
		PollInterval: 30 * time.Second,
	}
}

// NewClient creates an email client based on the provider in the config.
func NewClient(cfg *Config) (Client, error) {
	if cfg == nil {
		cfg = DefaultConfig()
	}
	switch cfg.Provider {
	case "imap", "":
		return NewIMAPSMTPClient(cfg), nil
	default:
		return NewIMAPSMTPClient(cfg), nil
	}
}
