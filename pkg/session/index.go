package session

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// SessionIndex manages the session metadata index file.
type SessionIndex struct {
	path string
	mu   sync.Mutex
}

// SessionMeta contains metadata about a single session.
type SessionMeta struct {
	ID           string    `json:"id"`
	AppName      string    `json:"appName"`
	UserID       string    `json:"userId"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Title        string    `json:"title,omitempty"`
	MessageCount int       `json:"messageCount"`
}

// IndexData is the top-level structure of the index file.
type IndexData struct {
	Version  int                    `json:"version"`
	Sessions map[string]SessionMeta `json:"sessions"`
}

// NewSessionIndex creates a SessionIndex at the given path.
func NewSessionIndex(path string) *SessionIndex {
	return &SessionIndex{path: path}
}

// Load reads the index from disk. Returns an empty index if the file doesn't exist.
func (idx *SessionIndex) Load() (*IndexData, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	return idx.loadUnsafe()
}

// loadUnsafe reads the index without locking (caller must hold the lock).
func (idx *SessionIndex) loadUnsafe() (*IndexData, error) {
	data, err := os.ReadFile(idx.path)
	if err != nil {
		if os.IsNotExist(err) {
			return &IndexData{
				Version:  1,
				Sessions: make(map[string]SessionMeta),
			}, nil
		}
		return nil, fmt.Errorf("failed to read index: %w", err)
	}

	var index IndexData
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("failed to parse index: %w", err)
	}

	if index.Sessions == nil {
		index.Sessions = make(map[string]SessionMeta)
	}

	return &index, nil
}

// Save writes the index to disk atomically.
func (idx *SessionIndex) Save(index *IndexData) error {
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize index: %w", err)
	}

	return atomicWrite(idx.path, data, 0644)
}

// Add adds a new session to the index.
func (idx *SessionIndex) Add(meta SessionMeta) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	index, err := idx.loadUnsafe()
	if err != nil {
		return err
	}

	index.Sessions[meta.ID] = meta
	return idx.Save(index)
}

// Remove removes a session from the index.
func (idx *SessionIndex) Remove(id string) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	index, err := idx.loadUnsafe()
	if err != nil {
		return err
	}

	delete(index.Sessions, id)
	return idx.Save(index)
}

// Update modifies a session in the index using the provided function.
func (idx *SessionIndex) Update(id string, fn func(*SessionMeta)) error {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	index, err := idx.loadUnsafe()
	if err != nil {
		return err
	}

	meta, ok := index.Sessions[id]
	if !ok {
		return fmt.Errorf("session %s not found in index", id)
	}

	fn(&meta)
	index.Sessions[id] = meta
	return idx.Save(index)
}

// Get retrieves metadata for a specific session.
func (idx *SessionIndex) Get(id string) (*SessionMeta, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	index, err := idx.loadUnsafe()
	if err != nil {
		return nil, err
	}

	meta, ok := index.Sessions[id]
	if !ok {
		return nil, fmt.Errorf("session %s not found", id)
	}

	return &meta, nil
}

// List returns all sessions matching the given app name and user ID.
// If userID is empty, all sessions for the app are returned.
func (idx *SessionIndex) List(appName, userID string) ([]SessionMeta, error) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	index, err := idx.loadUnsafe()
	if err != nil {
		return nil, err
	}

	var result []SessionMeta
	for _, meta := range index.Sessions {
		if meta.AppName != appName {
			continue
		}
		if userID != "" && meta.UserID != userID {
			continue
		}
		result = append(result, meta)
	}

	return result, nil
}
