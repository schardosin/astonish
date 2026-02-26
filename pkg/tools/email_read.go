package tools

import (
	"fmt"
	"time"

	"github.com/schardosin/astonish/pkg/email"
	"google.golang.org/adk/tool"
)

// --- email_list ---

// EmailListArgs is the input for email_list.
type EmailListArgs struct {
	Folder  string `json:"folder,omitempty" jsonschema:"IMAP folder to list. Default: INBOX. Others: Sent, Drafts, Spam, etc."`
	Unread  bool   `json:"unread,omitempty" jsonschema:"Only show unread messages"`
	From    string `json:"from,omitempty" jsonschema:"Filter by sender (substring match)"`
	Subject string `json:"subject,omitempty" jsonschema:"Filter by subject (substring match)"`
	Since   string `json:"since,omitempty" jsonschema:"Only messages after this date (ISO 8601, e.g. 2025-01-15)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Max results. Default: 20"`
}

// EmailListResult is the output of email_list.
type EmailListResult struct {
	Messages []emailSummaryJSON `json:"messages"`
	Total    int                `json:"total"`
}

type emailSummaryJSON struct {
	ID             string `json:"id"`
	From           string `json:"from"`
	To             string `json:"to"`
	Subject        string `json:"subject"`
	Date           string `json:"date"`
	Unread         bool   `json:"unread"`
	HasAttachments bool   `json:"has_attachments,omitempty"`
}

// EmailList lists emails in the inbox with optional filters.
func EmailList(client email.Client) func(tool.Context, EmailListArgs) (EmailListResult, error) {
	return func(ctx tool.Context, args EmailListArgs) (EmailListResult, error) {
		opts := email.ListOpts{
			Folder:  args.Folder,
			Unread:  args.Unread,
			From:    args.From,
			Subject: args.Subject,
			Limit:   args.Limit,
		}

		if args.Since != "" {
			t, err := time.Parse("2006-01-02", args.Since)
			if err != nil {
				t, err = time.Parse(time.RFC3339, args.Since)
				if err != nil {
					return EmailListResult{}, fmt.Errorf("invalid 'since' date format: %w", err)
				}
			}
			opts.Since = t
		}

		messages, err := client.ListMessages(ctx, opts)
		if err != nil {
			return EmailListResult{}, fmt.Errorf("failed to list emails: %w", err)
		}

		result := EmailListResult{
			Total: len(messages),
		}

		for _, m := range messages {
			to := ""
			if len(m.To) > 0 {
				to = m.To[0]
				if len(m.To) > 1 {
					to += fmt.Sprintf(" (+%d more)", len(m.To)-1)
				}
			}
			result.Messages = append(result.Messages, emailSummaryJSON{
				ID:             m.ID,
				From:           m.From,
				To:             to,
				Subject:        m.Subject,
				Date:           m.Date.Format(time.RFC3339),
				Unread:         m.Unread,
				HasAttachments: m.HasAttachments,
			})
		}

		return result, nil
	}
}

// --- email_read ---

// EmailReadArgs is the input for email_read.
type EmailReadArgs struct {
	ID string `json:"id" jsonschema:"Message ID (from email_list results)"`
}

// EmailReadResult is the output of email_read.
type EmailReadResult struct {
	From              string            `json:"from"`
	To                []string          `json:"to"`
	CC                []string          `json:"cc,omitempty"`
	Subject           string            `json:"subject"`
	Date              string            `json:"date"`
	Body              string            `json:"body"`
	HTML              string            `json:"html,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Links             []string          `json:"links,omitempty"`
	VerificationLinks []string          `json:"verification_links,omitempty"`
	Attachments       []attachmentJSON  `json:"attachments,omitempty"`
	Unread            bool              `json:"unread"`
}

type attachmentJSON struct {
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	ContentType string `json:"content_type"`
}

// EmailRead reads the full content of a specific email.
func EmailRead(client email.Client) func(tool.Context, EmailReadArgs) (EmailReadResult, error) {
	return func(ctx tool.Context, args EmailReadArgs) (EmailReadResult, error) {
		if args.ID == "" {
			return EmailReadResult{}, fmt.Errorf("id is required")
		}

		msg, err := client.ReadMessage(ctx, args.ID)
		if err != nil {
			return EmailReadResult{}, fmt.Errorf("failed to read email: %w", err)
		}

		result := EmailReadResult{
			From:              msg.From,
			To:                msg.To,
			CC:                msg.CC,
			Subject:           msg.Subject,
			Date:              msg.Date.Format(time.RFC3339),
			Body:              msg.Body,
			HTML:              msg.HTML,
			Headers:           msg.Headers,
			Links:             msg.Links,
			VerificationLinks: msg.VerificationLinks,
			Unread:            msg.Unread,
		}

		for _, att := range msg.Attachments {
			result.Attachments = append(result.Attachments, attachmentJSON{
				Name:        att.Name,
				Size:        att.Size,
				ContentType: att.ContentType,
			})
		}

		return result, nil
	}
}

// --- email_search ---

// EmailSearchArgs is the input for email_search.
type EmailSearchArgs struct {
	Query         string `json:"query,omitempty" jsonschema:"Free-text search (searches subject and sender)"`
	From          string `json:"from,omitempty" jsonschema:"Filter by sender"`
	To            string `json:"to,omitempty" jsonschema:"Filter by recipient"`
	Subject       string `json:"subject,omitempty" jsonschema:"Filter by subject"`
	Since         string `json:"since,omitempty" jsonschema:"Date range start (ISO 8601)"`
	Before        string `json:"before,omitempty" jsonschema:"Date range end (ISO 8601)"`
	HasAttachment bool   `json:"has_attachment,omitempty" jsonschema:"Only messages with attachments"`
	Folder        string `json:"folder,omitempty" jsonschema:"Folder to search. Default: INBOX"`
	Limit         int    `json:"limit,omitempty" jsonschema:"Max results. Default: 20"`
}

// EmailSearchResult is the output of email_search.
type EmailSearchResult struct {
	Messages []emailSummaryJSON `json:"messages"`
	Total    int                `json:"total"`
}

// EmailSearch searches emails by criteria.
func EmailSearch(client email.Client) func(tool.Context, EmailSearchArgs) (EmailSearchResult, error) {
	return func(ctx tool.Context, args EmailSearchArgs) (EmailSearchResult, error) {
		query := email.SearchQuery{
			Query:         args.Query,
			From:          args.From,
			To:            args.To,
			Subject:       args.Subject,
			HasAttachment: args.HasAttachment,
			Folder:        args.Folder,
			Limit:         args.Limit,
		}

		if args.Since != "" {
			t, err := parseDate(args.Since)
			if err != nil {
				return EmailSearchResult{}, fmt.Errorf("invalid 'since' date: %w", err)
			}
			query.Since = t
		}
		if args.Before != "" {
			t, err := parseDate(args.Before)
			if err != nil {
				return EmailSearchResult{}, fmt.Errorf("invalid 'before' date: %w", err)
			}
			query.Before = t
		}

		messages, err := client.SearchMessages(ctx, query)
		if err != nil {
			return EmailSearchResult{}, fmt.Errorf("failed to search emails: %w", err)
		}

		result := EmailSearchResult{Total: len(messages)}
		for _, m := range messages {
			to := ""
			if len(m.To) > 0 {
				to = m.To[0]
			}
			result.Messages = append(result.Messages, emailSummaryJSON{
				ID:             m.ID,
				From:           m.From,
				To:             to,
				Subject:        m.Subject,
				Date:           m.Date.Format(time.RFC3339),
				Unread:         m.Unread,
				HasAttachments: m.HasAttachments,
			})
		}

		return result, nil
	}
}

// parseDate tries ISO 8601 date and datetime formats.
func parseDate(s string) (time.Time, error) {
	t, err := time.Parse("2006-01-02", s)
	if err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
