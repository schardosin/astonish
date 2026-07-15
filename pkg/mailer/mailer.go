// Package mailer provides centralized email sending for the Astonish platform.
// Any package can import mailer and call Send() or SendAsync() after Init().
package mailer

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/SAP/astonish/pkg/email"
)

// ErrNotConfigured is returned when Send is called before Init or when no
// email client is available (e.g. personal mode without email configured).
var ErrNotConfigured = errors.New("mailer: email client not configured")

var (
	mu     sync.RWMutex
	client email.Client
)

// Init registers the email client used for all outbound system emails.
// Called once at daemon startup. Safe to call multiple times (last wins).
func Init(c email.Client) {
	mu.Lock()
	defer mu.Unlock()
	client = c
}

// IsConfigured returns true if Init has been called with a non-nil client.
func IsConfigured() bool {
	mu.RLock()
	defer mu.RUnlock()
	return client != nil
}

// Send delivers a message synchronously. Returns ErrNotConfigured if no
// email client is available. Use this when the caller needs to know whether
// delivery succeeded (e.g. verification codes).
func Send(ctx context.Context, msg Message) error {
	mu.RLock()
	c := client
	mu.RUnlock()

	if c == nil {
		return ErrNotConfigured
	}

	_, err := c.Send(ctx, email.OutgoingMessage{
		To:      msg.To(),
		Subject: msg.Subject(),
		Body:    msg.TextBody(),
		HTML:    msg.HTMLBody(),
	})
	return err
}

// SendAsync delivers a message in a background goroutine. Failures are logged
// as warnings but not surfaced to the caller. Use this for non-critical emails
// like welcome/invite notifications.
func SendAsync(ctx context.Context, msg Message) {
	mu.RLock()
	c := client
	mu.RUnlock()

	if c == nil {
		slog.Debug("mailer: skipping async send (not configured)", "to", msg.To(), "subject", msg.Subject())
		return
	}

	go func() {
		_, err := c.Send(context.Background(), email.OutgoingMessage{
			To:      msg.To(),
			Subject: msg.Subject(),
			Body:    msg.TextBody(),
			HTML:    msg.HTMLBody(),
		})
		if err != nil {
			slog.Warn("mailer: async send failed", "to", msg.To(), "subject", msg.Subject(), "error", err)
		}
	}()
}
