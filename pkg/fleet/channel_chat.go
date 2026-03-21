package fleet

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ChatChannel implements Channel using an in-memory message list with
// a condition variable for blocking reads. This is the "chat" channel type
// for fleet sessions where the communication happens in the Astonish UI.
//
// Human messages are posted by the API handler when the user sends a message.
// Agent messages are posted by the session manager after agent activation.
// SSE handlers subscribe via Subscribe/Unsubscribe to stream messages to the UI.
type ChatChannel struct {
	sessionID  string
	messages   []Message
	readCursor int // index of next message to return from WaitForMessage
	mu         sync.RWMutex
	cond       *sync.Cond
	closed     bool

	// subscribers is a map of subscriber ID -> channel for new messages.
	// Multiple SSE viewers can connect/disconnect independently.
	subscribers   map[string]chan Message
	subscribersMu sync.Mutex
}

// NewChatChannel creates a new chat channel for the given session.
func NewChatChannel(sessionID string) *ChatChannel {
	ch := &ChatChannel{
		sessionID:   sessionID,
		subscribers: make(map[string]chan Message),
	}
	ch.cond = sync.NewCond(&ch.mu)
	return ch
}

// Subscribe registers a new message subscriber and returns a channel that
// receives new messages as they are posted. The caller must call Unsubscribe
// when done to avoid leaking the goroutine/channel.
func (c *ChatChannel) Subscribe(id string) <-chan Message {
	c.subscribersMu.Lock()
	defer c.subscribersMu.Unlock()

	ch := make(chan Message, 100)
	c.subscribers[id] = ch
	return ch
}

// Unsubscribe removes a subscriber and closes its channel.
func (c *ChatChannel) Unsubscribe(id string) {
	c.subscribersMu.Lock()
	defer c.subscribersMu.Unlock()

	if ch, ok := c.subscribers[id]; ok {
		close(ch)
		delete(c.subscribers, id)
	}
}

// notifySubscribers sends a message to all active subscribers (non-blocking).
func (c *ChatChannel) notifySubscribers(msg Message) {
	c.subscribersMu.Lock()
	defer c.subscribersMu.Unlock()

	for _, ch := range c.subscribers {
		select {
		case ch <- msg:
		default:
			// subscriber is slow; drop message to avoid blocking the message loop
		}
	}
}

// PostMessage adds a message to the channel and notifies waiters.
func (c *ChatChannel) PostMessage(_ context.Context, msg Message) error {
	c.mu.Lock()

	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("channel is closed")
	}

	// Assign ID and timestamp if not set
	if msg.ID == "" {
		msg.ID = uuid.New().String()
	}
	if msg.Timestamp.IsZero() {
		msg.Timestamp = time.Now()
	}

	c.messages = append(c.messages, msg)

	// Wake up any goroutine blocked in WaitForMessage
	c.cond.Broadcast()

	c.mu.Unlock()

	// Notify subscribers outside the main lock to avoid deadlocks
	c.notifySubscribers(msg)

	return nil
}

// WaitForMessage blocks until a new message arrives that the Run loop hasn't
// seen yet. It uses a monotonically increasing read cursor so messages posted
// before the first call are NOT skipped.
func (c *ChatChannel) WaitForMessage(ctx context.Context) (Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Wait for a message beyond the current read cursor
	for c.readCursor >= len(c.messages) && !c.closed {
		// Release the lock and wait for signal, re-acquire on wake
		// We need to check context cancellation too, so we use a goroutine
		// to broadcast on context done.
		done := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				c.cond.Broadcast() // wake up the waiter
			case <-done:
			}
		}()

		c.cond.Wait()
		close(done)

		// Check if context was cancelled
		if ctx.Err() != nil {
			return Message{}, ctx.Err()
		}
	}

	if c.closed {
		return Message{}, fmt.Errorf("channel is closed")
	}

	// Return the next unread message and advance the cursor
	msg := c.messages[c.readCursor]
	c.readCursor++
	return msg, nil
}

// GetThread returns all messages in chronological order.
func (c *ChatChannel) GetThread(_ context.Context) ([]Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result := make([]Message, len(c.messages))
	copy(result, c.messages)
	return result, nil
}

// GetAgentMemory returns messages belonging to the given agent's memory,
// plus system/global messages (no MemoryKeys). If agentKey is empty, returns
// all messages (same as GetThread). Supports backward compat with old ThreadKey.
func (c *ChatChannel) GetAgentMemory(_ context.Context, agentKey string) ([]Message, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if agentKey == "" {
		result := make([]Message, len(c.messages))
		copy(result, c.messages)
		return result, nil
	}

	result := make([]Message, 0, len(c.messages))
	for _, msg := range c.messages {
		if msg.InAgentMemory(agentKey) {
			result = append(result, msg)
		}
	}
	return result, nil
}

// Close shuts down the channel and wakes up any blocked waiters.
func (c *ChatChannel) Close() error {
	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()

	// Close all subscriber channels
	c.subscribersMu.Lock()
	for id, ch := range c.subscribers {
		close(ch)
		delete(c.subscribers, id)
	}
	c.subscribersMu.Unlock()

	return nil
}

// SessionID returns the session ID this channel is associated with.
func (c *ChatChannel) SessionID() string {
	return c.sessionID
}

// MessageCount returns the current number of messages.
func (c *ChatChannel) MessageCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.messages)
}
