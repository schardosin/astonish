package session

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// ThreadIndex manages a persistent mapping from email Message-ID values to
// session keys. This enables per-thread email sessions: new emails create new
// sessions, and replies (identified by In-Reply-To / References headers) are
// routed to the same session as the original thread.
//
// The index file is stored alongside the session index as thread_index.json.
// It follows the same patterns: sync.Mutex for concurrency, load-modify-save
// for every mutation, and atomicWrite for crash safety.
type ThreadIndex struct {
	path string
	mu   sync.Mutex
}

// ThreadIndexData is the on-disk representation of the thread index.
type ThreadIndexData struct {
	Version int               `json:"version"`
	Threads map[string]string `json:"threads"` // Message-ID -> session key
}

// NewThreadIndex creates a ThreadIndex backed by the given file path.
func NewThreadIndex(path string) *ThreadIndex {
	return &ThreadIndex{path: path}
}

// Lookup returns the session key associated with a Message-ID, if any.
func (t *ThreadIndex) Lookup(messageID string) (string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := t.loadUnsafe()
	if err != nil {
		return "", false
	}

	sessionKey, ok := data.Threads[messageID]
	return sessionKey, ok
}

// LookupChain searches a list of Message-IDs (typically In-Reply-To followed
// by References, newest first) and returns the session key for the first match.
// This provides fallback: if In-Reply-To isn't indexed, an older Reference
// from the chain may still resolve.
func (t *ThreadIndex) LookupChain(messageIDs []string) (string, bool) {
	if len(messageIDs) == 0 {
		return "", false
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := t.loadUnsafe()
	if err != nil {
		return "", false
	}

	for _, id := range messageIDs {
		if sessionKey, ok := data.Threads[id]; ok {
			return sessionKey, true
		}
	}
	return "", false
}

// Associate maps one or more Message-IDs to a session key. Existing entries
// for the same Message-ID are overwritten. This is used to index both inbound
// (received) and outbound (sent) Message-IDs.
func (t *ThreadIndex) Associate(messageIDs []string, sessionKey string) error {
	if len(messageIDs) == 0 || sessionKey == "" {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := t.loadUnsafe()
	if err != nil {
		return err
	}

	for _, id := range messageIDs {
		if id != "" {
			data.Threads[id] = sessionKey
		}
	}

	return t.saveUnsafe(data)
}

// RemoveSession removes all thread mappings that point to the given session key.
// Called when a session is deleted (e.g., /new command) to clean up stale entries.
func (t *ThreadIndex) RemoveSession(sessionKey string) error {
	if sessionKey == "" {
		return nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	data, err := t.loadUnsafe()
	if err != nil {
		return err
	}

	changed := false
	for msgID, key := range data.Threads {
		if key == sessionKey {
			delete(data.Threads, msgID)
			changed = true
		}
	}

	if !changed {
		return nil
	}
	return t.saveUnsafe(data)
}

// Load reads the full index from disk.
func (t *ThreadIndex) Load() (*ThreadIndexData, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.loadUnsafe()
}

// loadUnsafe reads the index without locking (caller must hold the lock).
func (t *ThreadIndex) loadUnsafe() (*ThreadIndexData, error) {
	raw, err := os.ReadFile(t.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &ThreadIndexData{
				Version: 1,
				Threads: make(map[string]string),
			}, nil
		}
		return nil, fmt.Errorf("failed to read thread index: %w", err)
	}

	var data ThreadIndexData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("failed to parse thread index: %w", err)
	}

	if data.Threads == nil {
		data.Threads = make(map[string]string)
	}
	return &data, nil
}

// saveUnsafe writes the index to disk atomically (caller must hold the lock).
func (t *ThreadIndex) saveUnsafe(data *ThreadIndexData) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize thread index: %w", err)
	}
	return atomicWrite(t.path, raw, 0644)
}
