// Package api — file-backed session store for Studio device authorization.
//
// Authorized sessions are stored in ~/.config/astonish/web_sessions.json.
// Tokens are stored as SHA-256 hashes so that a leaked file does not
// expose usable session tokens.
package api

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	sessionFileName   = "web_sessions.json"
	defaultSessionTTL = 90 * 24 * time.Hour // 90 days
	tokenBytes        = 32                  // 256-bit random token
)

// AuthSession represents a single authorized browser session.
type AuthSession struct {
	TokenHash string    `json:"token_hash"` // SHA-256 hex of the raw token
	CreatedAt time.Time `json:"created_at"`
	LastSeen  time.Time `json:"last_seen"`
	UserAgent string    `json:"user_agent,omitempty"`
	IP        string    `json:"ip,omitempty"`
}

// AuthStore manages authorized web sessions on disk.
type AuthStore struct {
	mu       sync.RWMutex
	path     string
	ttl      time.Duration
	sessions map[string]*AuthSession // keyed by token hash
}

// NewAuthStore opens (or creates) the session store at the given config directory.
func NewAuthStore(configDir string, ttl time.Duration) (*AuthStore, error) {
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	storePath := filepath.Join(configDir, sessionFileName)
	s := &AuthStore{
		path:     storePath,
		ttl:      ttl,
		sessions: make(map[string]*AuthSession),
	}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load session store: %w", err)
	}
	return s, nil
}

// CreateSession generates a new random token, stores its hash, and returns
// the raw token (to be set as a cookie). The caller must not store the raw token.
func (s *AuthStore) CreateSession(userAgent, ip string) (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("failed to generate session token: %w", err)
	}
	token := hex.EncodeToString(raw)
	hash := hashToken(token)

	s.mu.Lock()
	s.sessions[hash] = &AuthSession{
		TokenHash: hash,
		CreatedAt: time.Now(),
		LastSeen:  time.Now(),
		UserAgent: userAgent,
		IP:        ip,
	}
	s.mu.Unlock()

	if err := s.save(); err != nil {
		return "", err
	}
	return token, nil
}

// ValidateToken checks whether a raw token corresponds to a valid, non-expired session.
// If valid, it updates LastSeen and returns true.
func (s *AuthStore) ValidateToken(token string) bool {
	hash := hashToken(token)
	now := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.sessions[hash]
	if !ok {
		return false
	}
	if now.Sub(sess.CreatedAt) > s.ttl {
		delete(s.sessions, hash)
		if err := s.saveLocked(); err != nil {
			slog.Error("failed to persist auth session store", "error", err)
		}
		return false
	}
	sess.LastSeen = now
	// Persist LastSeen update periodically (every 10 minutes) to avoid
	// excessive disk writes on every single request.
	if now.Sub(sess.LastSeen) > 10*time.Minute {
		if err := s.saveLocked(); err != nil {
			slog.Error("failed to persist auth session store", "error", err)
		}
	}
	return true
}

// SessionCount returns the number of active (non-expired) sessions.
func (s *AuthStore) SessionCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	count := 0
	now := time.Now()
	for _, sess := range s.sessions {
		if now.Sub(sess.CreatedAt) <= s.ttl {
			count++
		}
	}
	return count
}

// Cleanup removes all expired sessions from the store.
func (s *AuthStore) Cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for hash, sess := range s.sessions {
		if now.Sub(sess.CreatedAt) > s.ttl {
			delete(s.sessions, hash)
		}
	}
	if err := s.saveLocked(); err != nil {
		slog.Error("failed to persist auth session store", "error", err)
	}
}

// load reads sessions from disk. Must be called before use.
func (s *AuthStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	var sessions []*AuthSession
	if err := json.Unmarshal(data, &sessions); err != nil {
		return fmt.Errorf("corrupt session store: %w", err)
	}
	s.sessions = make(map[string]*AuthSession, len(sessions))
	now := time.Now()
	for _, sess := range sessions {
		// Skip expired on load
		if now.Sub(sess.CreatedAt) <= s.ttl {
			s.sessions[sess.TokenHash] = sess
		}
	}
	return nil
}

// save writes sessions to disk atomically. Caller must hold s.mu.
func (s *AuthStore) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.saveLocked()
}

// saveLocked writes sessions to disk. Caller must already hold s.mu (write or read).
func (s *AuthStore) saveLocked() error {
	sessions := make([]*AuthSession, 0, len(s.sessions))
	for _, sess := range s.sessions {
		sessions = append(sessions, sess)
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal sessions: %w", err)
	}
	return atomicWriteFile(s.path, data, 0600)
}

// hashToken returns the hex-encoded SHA-256 hash of a raw token string.
func hashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// atomicWriteFile writes data to a file atomically via temp-file + rename.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".astonish-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, perm); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	success = true
	return nil
}
