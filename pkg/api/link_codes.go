package api

import (
	"crypto/rand"
	"encoding/hex"
	"strings"
	"sync"
	"time"
)

// LinkCodeStore manages one-time codes for channel linking.
// Codes are generated when a user requests to link a channel (e.g., Telegram)
// and consumed when the external channel sends the code back (proving ownership).
type LinkCodeStore struct {
	mu    sync.RWMutex
	codes map[string]*PendingLink
}

// PendingLink is a pending channel link request awaiting verification.
type PendingLink struct {
	Code      string
	UserID    string // platform user ID
	Email     string // for the bot reply
	OrgSlug   string
	TeamSlug  string
	Channel   string // "telegram", "email"
	CreatedAt time.Time
	ExpiresAt time.Time
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
func (s *LinkCodeStore) Generate(userID, email, orgSlug, teamSlug, channel string) string {
	code := generateCode()

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
		OrgSlug:   orgSlug,
		TeamSlug:  teamSlug,
		Channel:   channel,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(5 * time.Minute),
	}
	return code
}

// Consume looks up a code, returns the pending link if valid, and removes it.
// Returns nil if the code is invalid or expired.
func (s *LinkCodeStore) Consume(code string) *PendingLink {
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

// generateCode creates a 6-character uppercase alphanumeric code.
func generateCode() string {
	b := make([]byte, 4) // 4 bytes = 8 hex chars, we take 6
	_, _ = rand.Read(b)
	return strings.ToUpper(hex.EncodeToString(b)[:6])
}
