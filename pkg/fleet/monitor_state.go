package fleet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MonitorStateStore abstracts persistence of fleet monitor state.
// In personal mode, this is backed by the filesystem.
// In platform mode, this is backed by the team's database table.
type MonitorStateStore interface {
	// LoadState loads the persisted state for a fleet plan's monitor.
	// Returns nil state (not an error) if no state exists yet.
	LoadState(planKey string) (*GitHubMonitorState, error)

	// SaveState persists the current monitor state for a fleet plan.
	SaveState(planKey string, state *GitHubMonitorState) error

	// DeleteState removes persisted state for a fleet plan (used during cleanup).
	DeleteState(planKey string) error
}

// FileMonitorStateStore persists monitor state as JSON files on disk.
// Used in personal mode. State files are stored at <dir>/.state/<planKey>.json.
type FileMonitorStateStore struct {
	dir string
}

// NewFileMonitorStateStore creates a file-based monitor state store.
func NewFileMonitorStateStore(dir string) *FileMonitorStateStore {
	return &FileMonitorStateStore{dir: dir}
}

func (f *FileMonitorStateStore) statePath(planKey string) string {
	return filepath.Join(f.dir, ".state", planKey+".json")
}

func (f *FileMonitorStateStore) LoadState(planKey string) (*GitHubMonitorState, error) {
	path := f.statePath(planKey)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no state yet
		}
		return nil, fmt.Errorf("reading monitor state: %w", err)
	}

	var state GitHubMonitorState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("parsing monitor state: %w", err)
	}

	if state.SeenIssues == nil {
		state.SeenIssues = make(map[int]*SeenIssueState)
	}
	return &state, nil
}

func (f *FileMonitorStateStore) SaveState(planKey string, state *GitHubMonitorState) error {
	path := f.statePath(planKey)

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating state directory: %w", err)
	}

	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling monitor state: %w", err)
	}

	// Atomic write via temp file
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("writing monitor state: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("renaming monitor state: %w", err)
	}

	return nil
}

func (f *FileMonitorStateStore) DeleteState(planKey string) error {
	path := f.statePath(planKey)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting monitor state: %w", err)
	}
	return nil
}
