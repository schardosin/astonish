package api

import (
	"context"
	"crypto/sha256"
	"sync"
	"time"

	"github.com/SAP/astonish/pkg/channels"
)

// LinkAttemptLimiter is process-local, shared by HTTP and daemon verification.
// Horizontal replicas multiply these budgets: deployments must additionally enforce
// a shared gateway/Redis budget before scaling verification workers. It bounds
// total attempts (including failures) without storing codes or unbounded sender IDs.
type LinkAttemptLimiter struct {
	mu       sync.Mutex
	window   time.Time
	channels map[channels.LinkChannel]int
	senders  map[[32]byte]int
}

const linkAttemptWindow = 5 * time.Minute
const linkSenderAttempts = 10
const linkChannelAttempts = 1000
const linkMaxSenders = 4096

func NewLinkAttemptLimiter() *LinkAttemptLimiter { return &LinkAttemptLimiter{} }
func (l *LinkAttemptLimiter) Allow(channel channels.LinkChannel, sender string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.window.IsZero() || now.Sub(l.window) >= linkAttemptWindow {
		l.window = now
		l.channels = make(map[channels.LinkChannel]int)
		l.senders = make(map[[32]byte]int)
	}
	key := sha256.Sum256([]byte(string(channel) + "\x00" + sender))
	if l.channels[channel] >= linkChannelAttempts || l.senders[key] >= linkSenderAttempts {
		return false
	}
	if _, exists := l.senders[key]; !exists && len(l.senders) >= linkMaxSenders {
		return false
	}
	l.channels[channel]++
	l.senders[key]++
	return true
}

var channelLinkAttempts = NewLinkAttemptLimiter()

// ConsumeChannelLinkCode validates the preselected principal before any consume.
// Pending codes are immutable; Consume must atomically delete and return at most
// one winner. Existing in-memory and Ent backends enforce single-winner deletion.
func ConsumeChannelLinkCode(ctx context.Context, backend LinkCodeBackend, channel channels.LinkChannel, principal, code string) *PendingLink {
	if !channelLinkAttempts.Allow(channel, principal) {
		return nil
	}
	if _, err := channels.ParseLinkPrincipal(channel, principal); err != nil {
		return nil
	}
	pending := backend.Peek(ctx, code)
	if pending == nil || !channels.MatchesLinkBinding(pending.Channel, channel, principal) {
		return nil
	}
	consumed := backend.Consume(ctx, code)
	if consumed == nil || consumed.UserID != pending.UserID || consumed.Channel != pending.Channel {
		return nil
	}
	return consumed
}
