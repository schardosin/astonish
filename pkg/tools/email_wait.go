package tools

import (
	"fmt"
	"time"

	"github.com/schardosin/astonish/pkg/email"
	"google.golang.org/adk/tool"
)

// EmailWaitArgs is the input for email_wait.
type EmailWaitArgs struct {
	From                string `json:"from,omitempty" jsonschema:"Substring match on sender address or display name (e.g. 'reddit' matches 'noreply@reddit.com'). At least one of from or subject is required."`
	Subject             string `json:"subject,omitempty" jsonschema:"Substring match on subject line (e.g. 'verify'). At least one of from or subject is required."`
	TimeoutSeconds      int    `json:"timeout_seconds,omitempty" jsonschema:"How long to wait for a matching email. Default: 120, Max: 300."`
	PollIntervalSeconds int    `json:"poll_interval_seconds,omitempty" jsonschema:"How often to check for new emails. Default: 5, Min: 3."`
}

// EmailWaitResult is the output of email_wait.
type EmailWaitResult struct {
	Found             bool             `json:"found"`
	ID                string           `json:"id,omitempty"`
	From              string           `json:"from,omitempty"`
	To                []string         `json:"to,omitempty"`
	CC                []string         `json:"cc,omitempty"`
	Subject           string           `json:"subject,omitempty"`
	Date              string           `json:"date,omitempty"`
	Body              string           `json:"body,omitempty"`
	HTML              string           `json:"html,omitempty"`
	Links             []string         `json:"links,omitempty"`
	VerificationLinks []string         `json:"verification_links,omitempty"`
	Attachments       []attachmentJSON `json:"attachments,omitempty"`
	WaitedSeconds     int              `json:"waited_seconds"`
	Message           string           `json:"message"`
}

// EmailWait polls the inbox for a matching email until it arrives or the
// timeout is reached. Designed for autonomous registration flows where the
// agent needs to wait for a verification email after signing up on a website.
func EmailWait(client email.Client) func(tool.Context, EmailWaitArgs) (EmailWaitResult, error) {
	return func(ctx tool.Context, args EmailWaitArgs) (EmailWaitResult, error) {
		if args.From == "" && args.Subject == "" {
			return EmailWaitResult{}, fmt.Errorf("at least one of 'from' or 'subject' is required")
		}

		timeout := args.TimeoutSeconds
		if timeout <= 0 {
			timeout = 120
		}
		if timeout > 300 {
			timeout = 300
		}

		pollInterval := args.PollIntervalSeconds
		if pollInterval < 3 {
			pollInterval = 5
		}

		// Only look for emails arriving from this moment forward to avoid
		// matching old messages already in the inbox.
		since := time.Now().Add(-1 * time.Minute)
		deadline := time.Now().Add(time.Duration(timeout) * time.Second)
		interval := time.Duration(pollInterval) * time.Second

		for {
			msgs, err := client.ListMessages(ctx, email.ListOpts{
				From:    args.From,
				Subject: args.Subject,
				Since:   since,
				Limit:   5,
			})
			if err != nil {
				return EmailWaitResult{}, fmt.Errorf("failed to check inbox: %w", err)
			}

			if len(msgs) > 0 {
				// Read the full content of the first match
				full, readErr := client.ReadMessage(ctx, msgs[0].ID)
				if readErr != nil {
					return EmailWaitResult{}, fmt.Errorf("found matching email but failed to read it: %w", readErr)
				}

				result := EmailWaitResult{
					Found:             true,
					ID:                full.ID,
					From:              full.From,
					To:                full.To,
					CC:                full.CC,
					Subject:           full.Subject,
					Date:              full.Date.Format(time.RFC3339),
					Body:              full.Body,
					HTML:              full.HTML,
					Links:             full.Links,
					VerificationLinks: full.VerificationLinks,
					WaitedSeconds:     int(time.Since(deadline.Add(-time.Duration(timeout) * time.Second)).Seconds()),
					Message:           "Matching email found",
				}
				for _, att := range full.Attachments {
					result.Attachments = append(result.Attachments, attachmentJSON{
						Name:        att.Name,
						Size:        att.Size,
						ContentType: att.ContentType,
					})
				}
				return result, nil
			}

			// Check if we've exceeded the deadline
			if time.Now().After(deadline) {
				waited := int(time.Duration(timeout) * time.Second / time.Second)
				desc := fmt.Sprintf("from=%q subject=%q", args.From, args.Subject)
				return EmailWaitResult{
					Found:         false,
					WaitedSeconds: waited,
					Message:       fmt.Sprintf("No matching email arrived within %d seconds (filters: %s)", timeout, desc),
				}, nil
			}

			// Check context cancellation
			select {
			case <-ctx.Done():
				return EmailWaitResult{}, ctx.Err()
			case <-time.After(interval):
				// Continue polling
			}
		}
	}
}
