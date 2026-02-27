package tools

import (
	"github.com/schardosin/astonish/pkg/email"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// emailClient is the shared email client used by all email tools.
// Set via SetEmailClient before tools are used.
var emailClient email.Client

// SetEmailClient configures the email client for all email tools.
func SetEmailClient(c email.Client) {
	emailClient = c
}

// HasEmailClient returns true if an email client has been set.
func HasEmailClient() bool {
	return emailClient != nil
}

// GetEmailTools creates all email tools sharing a single email client.
// Returns nil tools if no email client is configured.
func GetEmailTools() ([]tool.Tool, error) {
	if emailClient == nil {
		return nil, nil
	}

	// --- Read tools ---

	listTool, err := functiontool.New(functiontool.Config{
		Name:        "email_list",
		Description: "List emails in the inbox. Supports filtering by unread status, sender, subject, and date. Returns message summaries (not full bodies).",
	}, EmailList(emailClient))
	if err != nil {
		return nil, err
	}

	readTool, err := functiontool.New(functiontool.Config{
		Name:        "email_read",
		Description: "Read the full content of a specific email by ID. Returns body, headers, links, and extracted verification links (useful for portal registration flows).",
	}, EmailRead(emailClient))
	if err != nil {
		return nil, err
	}

	searchTool, err := functiontool.New(functiontool.Config{
		Name:        "email_search",
		Description: "Search emails by free-text query, sender, recipient, subject, date range, and attachment presence.",
	}, EmailSearch(emailClient))
	if err != nil {
		return nil, err
	}

	// --- Send tools ---

	sendTool, err := functiontool.New(functiontool.Config{
		Name:        "email_send",
		Description: "Compose and send a new email. Supports plain text and optional HTML body.",
	}, EmailSend(emailClient))
	if err != nil {
		return nil, err
	}

	replyTool, err := functiontool.New(functiontool.Config{
		Name:        "email_reply",
		Description: "Reply to an existing email, preserving threading (In-Reply-To and References headers). Can reply to sender only or reply-all.",
	}, EmailReply(emailClient))
	if err != nil {
		return nil, err
	}

	// --- Manage tools ---

	markReadTool, err := functiontool.New(functiontool.Config{
		Name:        "email_mark_read",
		Description: "Mark emails as read or unread.",
	}, EmailMarkRead(emailClient))
	if err != nil {
		return nil, err
	}

	deleteTool, err := functiontool.New(functiontool.Config{
		Name:        "email_delete",
		Description: "Delete emails (move to trash, or permanently delete).",
	}, EmailDelete(emailClient))
	if err != nil {
		return nil, err
	}

	// --- Wait tool (for autonomous flows) ---

	waitTool, err := functiontool.New(functiontool.Config{
		Name:        "email_wait",
		Description: "Wait for a matching email to arrive (polls inbox). Use for registration flows where you need to receive a verification or confirmation email. Supports substring matching on sender and subject. Returns the full message content with extracted verification links when found.",
	}, EmailWait(emailClient))
	if err != nil {
		return nil, err
	}

	return []tool.Tool{
		listTool,
		readTool,
		searchTool,
		sendTool,
		replyTool,
		markReadTool,
		deleteTool,
		waitTool,
	}, nil
}
