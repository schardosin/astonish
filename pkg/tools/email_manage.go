package tools

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/email"
	"google.golang.org/adk/tool"
)

// --- email_mark_read ---

// EmailMarkReadArgs is the input for email_mark_read.
type EmailMarkReadArgs struct {
	IDs    []string `json:"ids" jsonschema:"Message IDs to update (required)"`
	Unread bool     `json:"unread,omitempty" jsonschema:"If true, mark as unread instead. Default: false (mark as read)"`
}

// EmailMarkReadResult is the output of email_mark_read.
type EmailMarkReadResult struct {
	Success bool `json:"success"`
	Count   int  `json:"count"`
}

// EmailMarkRead marks emails as read or unread.
func EmailMarkRead(client email.Client) func(tool.Context, EmailMarkReadArgs) (EmailMarkReadResult, error) {
	return func(ctx tool.Context, args EmailMarkReadArgs) (EmailMarkReadResult, error) {
		if len(args.IDs) == 0 {
			return EmailMarkReadResult{}, fmt.Errorf("'ids' is required — specify at least one message ID")
		}

		var err error
		if args.Unread {
			err = client.MarkUnread(ctx, args.IDs)
		} else {
			err = client.MarkRead(ctx, args.IDs)
		}
		if err != nil {
			return EmailMarkReadResult{}, fmt.Errorf("failed to update read status: %w", err)
		}

		return EmailMarkReadResult{
			Success: true,
			Count:   len(args.IDs),
		}, nil
	}
}

// --- email_delete ---

// EmailDeleteArgs is the input for email_delete.
type EmailDeleteArgs struct {
	IDs       []string `json:"ids" jsonschema:"Message IDs to delete (required)"`
	Permanent bool     `json:"permanent,omitempty" jsonschema:"Skip trash, delete permanently. Default: false"`
}

// EmailDeleteResult is the output of email_delete.
type EmailDeleteResult struct {
	Success bool `json:"success"`
	Count   int  `json:"count"`
}

// EmailDelete deletes emails (moves to trash or permanently deletes).
func EmailDelete(client email.Client) func(tool.Context, EmailDeleteArgs) (EmailDeleteResult, error) {
	return func(ctx tool.Context, args EmailDeleteArgs) (EmailDeleteResult, error) {
		if len(args.IDs) == 0 {
			return EmailDeleteResult{}, fmt.Errorf("'ids' is required — specify at least one message ID")
		}

		err := client.Delete(ctx, args.IDs, args.Permanent)
		if err != nil {
			return EmailDeleteResult{}, fmt.Errorf("failed to delete emails: %w", err)
		}

		return EmailDeleteResult{
			Success: true,
			Count:   len(args.IDs),
		}, nil
	}
}
