package fleet

import (
	"context"
)

// Channel is the communication backbone for a fleet session.
// It abstracts the medium through which agents and the human exchange messages.
// Implementations include ChatChannel (in-memory for UI sessions) and
// GitHubIssueChannel (posts comments to a GitHub issue).
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

// Subscribable is an optional interface that Channel implementations can
// provide to support real-time message streaming to multiple viewers (SSE).
// Both ChatChannel and GitHubIssueChannel implement this.
type Subscribable interface {
	Subscribe(id string) <-chan Message
	Unsubscribe(id string)
}

// ExternalPoster is an optional interface for channels that post messages to
// an external system (e.g., GitHub issue comments). The Run loop uses this to
// control WHEN external posting happens — specifically, to defer it until
// after routing decides whether the message is part of a self-routing chain.
//
// PostMessage always adds to the internal thread and notifies SSE subscribers,
// but never posts externally. The caller must explicitly call PostExternal
// for messages that should appear on the external system.
type ExternalPoster interface {
	PostExternal(msg Message)
}
