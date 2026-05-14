package filestore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/schardosin/astonish/pkg/store"
)

// sandboxSessionStore is the personal-mode implementation of
// store.SandboxSessionStore. It persists sandbox-session metadata to a JSON
// file (sandbox_sessions.json) in the caller-supplied data directory.
//
// During Phase A the legacy pkg/sandbox.SessionRegistry continues to own
// sessions.json (the Incus path uses it for container→session reverse
// lookups and reaping). The new file name avoids clobbering that file until
// the cutover step migrates SessionRegistry onto this interface.
//
// The JSON layout is a map keyed by session_id for O(1) Get; List enumerates
// values and sorts by CreatedAt DESC to match the documented contract.
type sandboxSessionStore struct {
	mu       sync.RWMutex
	entries  map[string]*store.SandboxSession
	filePath string
}

// NewSandboxSessionStore constructs a personal-mode session store rooted at
// dataDir. The JSON file is lazily created on the first mutating call; if it
// already exists, its contents are loaded immediately. A non-existent file
// is not an error (empty store).
func NewSandboxSessionStore(dataDir string) (store.SandboxSessionStore, error) {
	if dataDir == "" {
		return nil, errors.New("sandbox session store: dataDir is required")
	}
	s := &sandboxSessionStore{
		entries:  make(map[string]*store.SandboxSession),
		filePath: filepath.Join(dataDir, "sandbox_sessions.json"),
	}
	if err := s.load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("sandbox session store: load %s: %w", s.filePath, err)
	}
	return s, nil
}

// load reads the JSON file; caller must not hold the lock.
func (s *sandboxSessionStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.filePath)
	if err != nil {
		return err
	}
	var rows []*store.SandboxSession
	if err := json.Unmarshal(data, &rows); err != nil {
		return fmt.Errorf("parse sandbox_sessions.json: %w", err)
	}
	s.entries = make(map[string]*store.SandboxSession, len(rows))
	for _, r := range rows {
		if r == nil || r.SessionID == "" {
			continue
		}
		cp := *r
		s.entries[cp.SessionID] = &cp
	}
	return nil
}

// saveLocked writes the store to disk; caller must hold s.mu (read or write).
// The file is written atomically via a tmp file + rename to guard against
// partial writes during a crash.
func (s *sandboxSessionStore) saveLocked() error {
	rows := make([]*store.SandboxSession, 0, len(s.entries))
	for _, e := range s.entries {
		rows = append(rows, e)
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].CreatedAt.After(rows[j].CreatedAt)
	})
	data, err := json.MarshalIndent(rows, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sandbox_sessions.json: %w", err)
	}
	dir := filepath.Dir(s.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".sandbox_sessions.json.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, s.filePath)
}

// --------------------------------------------------------------------------
// SandboxSessionStore implementation
// --------------------------------------------------------------------------

func (s *sandboxSessionStore) Put(_ context.Context, sess *store.SandboxSession) error {
	if sess == nil {
		return errors.New("sandbox session is nil")
	}
	if sess.SessionID == "" {
		return errors.New("sandbox session: SessionID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	cp := *sess
	if existing, ok := s.entries[cp.SessionID]; ok {
		// Preserve earliest CreatedAt across replaces.
		if !existing.CreatedAt.IsZero() && (cp.CreatedAt.IsZero() || existing.CreatedAt.Before(cp.CreatedAt)) {
			cp.CreatedAt = existing.CreatedAt
		}
	}
	if cp.CreatedAt.IsZero() {
		cp.CreatedAt = now
	}
	if cp.LastActiveAt.IsZero() {
		cp.LastActiveAt = now
	}
	cp.UpdatedAt = now
	if cp.Backend == "" {
		cp.Backend = "incus"
	}
	if cp.State == "" {
		cp.State = store.SandboxSessionStateCreating
	}
	if cp.ChatSessionID == "" {
		// Personal-mode convention: chat_session_id defaults to session_id.
		cp.ChatSessionID = cp.SessionID
	}
	s.entries[cp.SessionID] = &cp
	return s.saveLocked()
}

func (s *sandboxSessionStore) Get(_ context.Context, sessionID string) (*store.SandboxSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.entries[sessionID]
	if !ok {
		return nil, nil
	}
	cp := *e
	return &cp, nil
}

func (s *sandboxSessionStore) GetByContainerName(_ context.Context, containerName string) (*store.SandboxSession, error) {
	if containerName == "" {
		return nil, nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, e := range s.entries {
		if e.ContainerName == containerName {
			cp := *e
			return &cp, nil
		}
	}
	return nil, nil
}

func (s *sandboxSessionStore) List(_ context.Context, filter store.SandboxSessionFilter) ([]*store.SandboxSession, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*store.SandboxSession, 0, len(s.entries))
	for _, e := range s.entries {
		if filter.State != "" && e.State != filter.State {
			continue
		}
		if filter.CreatedBy != "" && e.CreatedBy != filter.CreatedBy {
			continue
		}
		if filter.Pinned != nil && e.Pinned != *filter.Pinned {
			continue
		}
		if filter.ContainerName != "" && e.ContainerName != filter.ContainerName {
			continue
		}
		cp := *e
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out, nil
}

// Delete is idempotent: removing a missing session is not an error (matches
// the pkg/sandbox.SessionRegistry.Remove contract that predates this store).
func (s *sandboxSessionStore) Delete(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.entries[sessionID]; !ok {
		return nil
	}
	delete(s.entries, sessionID)
	return s.saveLocked()
}

func (s *sandboxSessionStore) UpdateState(_ context.Context, sessionID string, state store.SandboxSessionState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[sessionID]
	if !ok {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	now := time.Now().UTC()
	e.State = state
	e.UpdatedAt = now
	e.LastActiveAt = now
	return s.saveLocked()
}

func (s *sandboxSessionStore) UpdatePorts(_ context.Context, sessionID string, ports []int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[sessionID]
	if !ok {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	// Copy the slice so the caller can't mutate our state after the call.
	if len(ports) == 0 {
		e.ExposedPorts = nil
	} else {
		cp := make([]int, len(ports))
		copy(cp, ports)
		e.ExposedPorts = cp
	}
	e.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

func (s *sandboxSessionStore) SetBaseDomain(_ context.Context, sessionID, baseDomain string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[sessionID]
	if !ok {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	e.BaseDomain = baseDomain
	e.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

func (s *sandboxSessionStore) SetPinned(_ context.Context, sessionID string, pinned bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[sessionID]
	if !ok {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	e.Pinned = pinned
	e.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

func (s *sandboxSessionStore) SetUpperLayer(_ context.Context, sessionID, upperLayerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[sessionID]
	if !ok {
		return fmt.Errorf("sandbox session %s not found", sessionID)
	}
	e.UpperLayerID = upperLayerID
	e.UpdatedAt = time.Now().UTC()
	return s.saveLocked()
}

// Compile-time assertion.
var _ store.SandboxSessionStore = (*sandboxSessionStore)(nil)
