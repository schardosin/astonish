package sandbox

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// SessionEntry maps a session to its container.
type SessionEntry struct {
	SessionID     string    `json:"session_id"`
	ContainerName string    `json:"container_name"`
	TemplateName  string    `json:"template_name"`
	CreatedAt     time.Time `json:"created_at"`
}

// SessionRegistry maps session IDs to container names with JSON persistence.
// It is safe for concurrent access.
type SessionRegistry struct {
	mu       sync.RWMutex
	entries  map[string]*SessionEntry
	filePath string
}

// NewSessionRegistry creates a new registry backed by a JSON file.
func NewSessionRegistry() (*SessionRegistry, error) {
	dataDir, err := sandboxDataDir()
	if err != nil {
		return nil, err
	}

	r := &SessionRegistry{
		entries:  make(map[string]*SessionEntry),
		filePath: filepath.Join(dataDir, "sessions.json"),
	}

	if err := r.Load(); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to load session registry: %w", err)
	}

	return r, nil
}

// Load reads the session registry from disk.
func (r *SessionRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.filePath)
	if err != nil {
		return err
	}

	var entries []*SessionEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("failed to parse session registry: %w", err)
	}

	r.entries = make(map[string]*SessionEntry)
	for _, e := range entries {
		r.entries[e.SessionID] = e
	}

	return nil
}

// Save writes the session registry to disk.
func (r *SessionRegistry) Save() error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entries := make([]*SessionEntry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal session registry: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(r.filePath), 0755); err != nil {
		return fmt.Errorf("failed to create registry directory: %w", err)
	}

	return os.WriteFile(r.filePath, data, 0644)
}

// Put registers a session-to-container mapping and saves.
func (r *SessionRegistry) Put(sessionID, containerName, templateName string) error {
	r.mu.Lock()
	r.entries[sessionID] = &SessionEntry{
		SessionID:     sessionID,
		ContainerName: containerName,
		TemplateName:  templateName,
		CreatedAt:     time.Now(),
	}
	r.mu.Unlock()
	return r.Save()
}

// Get returns the session entry, or nil if not found.
func (r *SessionRegistry) Get(sessionID string) *SessionEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries[sessionID]
}

// GetContainerName returns the container name for a session, or empty string.
func (r *SessionRegistry) GetContainerName(sessionID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if e, ok := r.entries[sessionID]; ok {
		return e.ContainerName
	}
	return ""
}

// Remove deletes a session-to-container mapping and saves.
func (r *SessionRegistry) Remove(sessionID string) error {
	r.mu.Lock()
	delete(r.entries, sessionID)
	r.mu.Unlock()
	return r.Save()
}

// List returns all session entries.
func (r *SessionRegistry) List() []*SessionEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*SessionEntry, 0, len(r.entries))
	for _, e := range r.entries {
		result = append(result, e)
	}

	return result
}

// SessionIDs returns all registered session IDs.
func (r *SessionRegistry) SessionIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.entries))
	for id := range r.entries {
		ids = append(ids, id)
	}

	return ids
}

// ResolveSessionID resolves a user-provided identifier to a session ID.
// It accepts: exact session ID, container name, session ID prefix, or
// container name prefix. Returns the full session ID and true if found.
func (r *SessionRegistry) ResolveSessionID(input string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Exact session ID match
	if _, ok := r.entries[input]; ok {
		return input, true
	}

	// Match by container name, session ID prefix, or container name prefix
	for sessID, entry := range r.entries {
		if entry.ContainerName == input {
			return sessID, true
		}
		if strings.HasPrefix(sessID, input) {
			return sessID, true
		}
		if strings.HasPrefix(entry.ContainerName, input) {
			return sessID, true
		}
	}

	return "", false
}
