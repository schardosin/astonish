package fleet

import (
	"context"
)

// Channel is the communication backbone for a fleet session.
// It abstracts the medium through which agents and the human exchange messages.
// The initial implementation uses an Astonish chat session as the channel;
// future implementations could use GitHub Issues, Jira, Slack, etc.
type Channel interface {
	// PostMessage posts a message to the channel.
	// The message is persisted and visible to all participants.
	PostMessage(ctx context.Context, msg Message) error

	// WaitForMessage blocks until a new message arrives on the channel.
	// Returns the message or an error (e.g., context cancelled, channel closed).
	WaitForMessage(ctx context.Context) (Message, error)

	// GetThread returns all messages in the current thread, ordered chronologically.
	GetThread(ctx context.Context) ([]Message, error)

	// Close shuts down the channel and releases resources.
	Close() error
}
