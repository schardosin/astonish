package api

import (
	"context"
	"fmt"
	"sync"

	emailpkg "github.com/schardosin/astonish/pkg/email"
)

// emailClient holds a reference to the daemon's configured email client.
// Set at startup via SetEmailClient. Used to send verification emails.
var (
	emailClientMu sync.RWMutex
	emailClient   emailpkg.Client
)

// SetEmailClient stores a reference to the daemon's email client for
// sending verification emails during channel linking.
func SetEmailClient(c emailpkg.Client) {
	emailClientMu.Lock()
	defer emailClientMu.Unlock()
	emailClient = c
}

// getEmailClient returns the configured email client, or nil if not set.
func getEmailClient() emailpkg.Client {
	emailClientMu.RLock()
	defer emailClientMu.RUnlock()
	return emailClient
}

// sendEmailVerificationCode sends a verification email containing the 6-char
// code to the target email address. The email is sent FROM the bot's configured
// address (e.g., astonishbot@gmail.com) TO the user's personal email.
func sendEmailVerificationCode(ctx context.Context, toAddr, code string) error {
	client := getEmailClient()
	if client == nil {
		return fmt.Errorf("email channel is not configured")
	}

	subject := "Verify your email for Astonish"
	body := fmt.Sprintf(`Hi,

Your email verification code is:

    %s

Enter this code in Settings → Channels to complete linking your email.

This code expires in 5 minutes.

— Astonish Bot`, code)

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<body style="font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; max-width: 480px; margin: 0 auto; padding: 20px;">
<p>Hi,</p>
<p>Your email verification code is:</p>
<div style="text-align: center; margin: 24px 0;">
  <span style="font-size: 32px; font-weight: bold; letter-spacing: 6px; font-family: monospace; background: #f3f4f6; padding: 12px 24px; border-radius: 8px; border: 1px solid #e5e7eb;">%s</span>
</div>
<p>Enter this code in <strong>Settings → Channels</strong> to complete linking your email.</p>
<p style="color: #6b7280; font-size: 13px;">This code expires in 5 minutes.</p>
<hr style="border: none; border-top: 1px solid #e5e7eb; margin: 24px 0;">
<p style="color: #9ca3af; font-size: 12px;">— Astonish Bot</p>
</body>
</html>`, code)

	_, err := client.Send(ctx, emailpkg.OutgoingMessage{
		To:      []string{toAddr},
		Subject: subject,
		Body:    body,
		HTML:    html,
	})
	return err
}
