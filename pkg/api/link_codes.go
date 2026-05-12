package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// LinkCodeBackend abstracts link code storage for channel linking flows.
// In personal mode (or when PG is unavailable), the in-memory implementation is used.
// In platform mode, the PG-backed implementation enables stateless horizontal scaling.
type LinkCodeBackend interface {
	Generate(ctx context.Context, userID, email, channel string) (string, error)
	Consume(ctx context.Context, code string) *PendingLink
}

// PendingLink is a pending channel link request awaiting verification.
type PendingLink struct {
	Code      string
	UserID    string // platform user ID
	Email     string // for the bot reply
	Channel   string // "telegram", "email:<address>"
	CreatedAt time.Time
	ExpiresAt time.Time
}

// LinkCodeStore is the in-memory implementation of LinkCodeBackend.
// Manages one-time codes for channel linking.
// Codes are generated when a user requests to link a channel (e.g., Telegram)
// and consumed when the external channel sends the code back (proving ownership).
type LinkCodeStore struct {
	mu    sync.RWMutex
	codes map[string]*PendingLink
}

// NewLinkCodeStore creates a new link code store with automatic cleanup.
func NewLinkCodeStore() *LinkCodeStore {
	s := &LinkCodeStore{
		codes: make(map[string]*PendingLink),
	}
	// Background cleanup of expired codes every 60s
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			s.cleanup()
		}
	}()
	return s
}

// Generate creates a new link code for the given user/channel.
// Returns the 6-character alphanumeric code.
func (s *LinkCodeStore) Generate(_ context.Context, userID, email, channel string) (string, error) {
	code := generateLinkCode()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Remove any existing pending code for this user+channel combo
	for k, v := range s.codes {
		if v.UserID == userID && v.Channel == channel {
			delete(s.codes, k)
		}
	}

	s.codes[code] = &PendingLink{
		Code:      code,
		UserID:    userID,
		Email:     email,
		Channel:   channel,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	return code, nil
}

// Consume looks up a code, returns the pending link if valid, and removes it.
// Returns nil if the code is invalid or expired.
func (s *LinkCodeStore) Consume(_ context.Context, code string) *PendingLink {
	code = strings.ToUpper(strings.TrimSpace(code))

	s.mu.Lock()
	defer s.mu.Unlock()

	link, ok := s.codes[code]
	if !ok {
		return nil
	}
	if time.Now().After(link.ExpiresAt) {
		delete(s.codes, code)
		return nil
	}
	delete(s.codes, code)
	return link
}

// cleanup removes expired codes.
func (s *LinkCodeStore) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for k, v := range s.codes {
		if now.After(v.ExpiresAt) {
			delete(s.codes, k)
		}
	}
}

// generateLinkCode creates a 6-character uppercase alphanumeric code.
func generateLinkCode() string {
	b := make([]byte, 4) // 4 bytes = 8 hex chars, we take 6
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b)[:6])
}

// Compile-time interface check
var _ LinkCodeBackend = (*LinkCodeStore)(nil)
