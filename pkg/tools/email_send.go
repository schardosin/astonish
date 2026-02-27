package tools

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/email"
	"google.golang.org/adk/tool"
)

// --- email_send ---

// EmailSendArgs is the input for email_send.
type EmailSendArgs struct {
	To      []string `json:"to" jsonschema:"Recipient email addresses (required)"`
	CC      []string `json:"cc,omitempty" jsonschema:"CC recipient addresses"`
	Subject string   `json:"subject" jsonschema:"Email subject (required)"`
	Body    string   `json:"body" jsonschema:"Plain text email body (required)"`
	HTML    string   `json:"html,omitempty" jsonschema:"Optional HTML body. If omitted, plain text is used."`
	ReplyTo string   `json:"reply_to,omitempty" jsonschema:"Optional Reply-To address"`
}

// EmailSendResult is the output of email_send.
type EmailSendResult struct {
	MessageID string `json:"message_id"`
	Success   bool   `json:"success"`
}

// EmailSend composes and sends a new email.
func EmailSend(client email.Client) func(tool.Context, EmailSendArgs) (EmailSendResult, error) {
	return func(ctx tool.Context, args EmailSendArgs) (EmailSendResult, error) {
		if len(args.To) == 0 {
			return EmailSendResult{}, fmt.Errorf("'to' is required — specify at least one recipient")
		}
		if args.Subject == "" {
			return EmailSendResult{}, fmt.Errorf("'subject' is required")
		}
		if args.Body == "" {
			return EmailSendResult{}, fmt.Errorf("'body' is required")
		}

		msg := email.OutgoingMessage{
			To:      args.To,
			CC:      args.CC,
			Subject: args.Subject,
			Body:    args.Body,
			HTML:    args.HTML,
			ReplyTo: args.ReplyTo,
		}

		messageID, err := client.Send(ctx, msg)
		if err != nil {
			return EmailSendResult{}, fmt.Errorf("failed to send email: %w", err)
		}

		return EmailSendResult{
			MessageID: messageID,
			Success:   true,
		}, nil
	}
}

// --- email_reply ---

// EmailReplyArgs is the input for email_reply.
type EmailReplyArgs struct {
	ID       string `json:"id" jsonschema:"Message ID to reply to (from email_list or email_read)"`
	Body     string `json:"body" jsonschema:"Reply body text (required)"`
	HTML     string `json:"html,omitempty" jsonschema:"Optional HTML reply body"`
	ReplyAll bool   `json:"reply_all,omitempty" jsonschema:"Reply to all recipients. Default: false (reply to sender only)"`
}

// EmailReplyResult is the output of email_reply.
type EmailReplyResult struct {
	MessageID string `json:"message_id"`
	Success   bool   `json:"success"`
}

// EmailReply replies to an existing email, preserving threading.
func EmailReply(client email.Client) func(tool.Context, EmailReplyArgs) (EmailReplyResult, error) {
	return func(ctx tool.Context, args EmailReplyArgs) (EmailReplyResult, error) {
		if args.ID == "" {
			return EmailReplyResult{}, fmt.Errorf("'id' is required — specify the message to reply to")
		}
		if args.Body == "" {
			return EmailReplyResult{}, fmt.Errorf("'body' is required")
		}

		msg := email.OutgoingMessage{
			Body: args.Body,
			HTML: args.HTML,
		}

		messageID, err := client.Reply(ctx, args.ID, args.ReplyAll, msg)
		if err != nil {
			return EmailReplyResult{}, fmt.Errorf("failed to send reply: %w", err)
		}

		return EmailReplyResult{
			MessageID: messageID,
			Success:   true,
		}, nil
	}
}
