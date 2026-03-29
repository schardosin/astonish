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
	ExposedPorts  []int     `json:"exposed_ports,omitempty"`
	BaseDomain    string    `json:"base_domain,omitempty"`
	Pinned        bool      `json:"pinned,omitempty"`
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
	return r.saveLocked()
}

// saveLocked writes the session registry to disk. Caller must hold r.mu (read or write).
func (r *SessionRegistry) saveLocked() error {
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
	defer r.mu.Unlock()
	r.entries[sessionID] = &SessionEntry{
		SessionID:     sessionID,
		ContainerName: containerName,
		TemplateName:  templateName,
		CreatedAt:     time.Now(),
	}
	return r.saveLocked()
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
	defer r.mu.Unlock()
	delete(r.entries, sessionID)
	return r.saveLocked()
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

// Reap removes registry entries whose containers no longer exist in Incus.
// Returns the number of entries removed. This is the self-healing mechanism
// that prevents "missing" entries from accumulating after container destruction
// by code paths that don't clean the registry (e.g., LazyNodeClient.Cleanup,
// fleet session exit).
func (r *SessionRegistry) Reap(client *IncusClient) int {
	r.mu.Lock()
	defer r.mu.Unlock()

	var stale []string
	for sessID, entry := range r.entries {
		if !client.InstanceExists(entry.ContainerName) {
			stale = append(stale, sessID)
		}
	}

	if len(stale) == 0 {
		return 0
	}

	for _, sessID := range stale {
		delete(r.entries, sessID)
	}

	_ = r.saveLocked()
	return len(stale)
}

// GetByContainerName returns the session entry for a given container name, or nil.
func (r *SessionRegistry) GetByContainerName(containerName string) *SessionEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, e := range r.entries {
		if e.ContainerName == containerName {
			return e
		}
	}
	return nil
}

// ExposePort adds a port to the exposed ports list for a container.
// Returns true if the port was newly added, false if already exposed.
func (r *SessionRegistry) ExposePort(containerName string, port int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var entry *SessionEntry
	for _, e := range r.entries {
		if e.ContainerName == containerName {
			entry = e
			break
		}
	}
	if entry == nil {
		return false, fmt.Errorf("container %q not found in registry", containerName)
	}

	// Check for duplicate
	for _, p := range entry.ExposedPorts {
		if p == port {
			return false, nil
		}
	}

	entry.ExposedPorts = append(entry.ExposedPorts, port)
	return true, r.saveLocked()
}

// UnexposePort removes a port from the exposed ports list for a container.
// Returns true if the port was found and removed, false if it was not exposed.
func (r *SessionRegistry) UnexposePort(containerName string, port int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var entry *SessionEntry
	for _, e := range r.entries {
		if e.ContainerName == containerName {
			entry = e
			break
		}
	}
	if entry == nil {
		return false, fmt.Errorf("container %q not found in registry", containerName)
	}

	for i, p := range entry.ExposedPorts {
		if p == port {
			entry.ExposedPorts = append(entry.ExposedPorts[:i], entry.ExposedPorts[i+1:]...)
			return true, r.saveLocked()
		}
	}

	return false, nil
}

// IsPortExposed checks if a specific port is exposed on a container.
func (r *SessionRegistry) IsPortExposed(containerName string, port int) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.ContainerName == containerName {
			for _, p := range e.ExposedPorts {
				if p == port {
					return true
				}
			}
			return false
		}
	}
	return false
}

// SetBaseDomain sets the base domain for a container's session entry and saves.
// The base domain is used to construct subdomain proxy hostnames.
func (r *SessionRegistry) SetBaseDomain(containerName, baseDomain string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.entries {
		if e.ContainerName == containerName {
			e.BaseDomain = baseDomain
			return r.saveLocked()
		}
	}
	return fmt.Errorf("container %q not found in registry", containerName)
}

// GetBaseDomain returns the stored base domain for a container, or empty string.
func (r *SessionRegistry) GetBaseDomain(containerName string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, e := range r.entries {
		if e.ContainerName == containerName {
			return e.BaseDomain
		}
	}
	return ""
}

// SetPinned marks a container as pinned (exempt from orphan cleanup) and saves.
// Pinned containers are manually created via `sandbox create` and should not be
// destroyed by automatic cleanup cycles.
func (r *SessionRegistry) SetPinned(containerName string, pinned bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, e := range r.entries {
		if e.ContainerName == containerName {
			e.Pinned = pinned
			return r.saveLocked()
		}
	}
	return fmt.Errorf("container %q not found in registry", containerName)
}
