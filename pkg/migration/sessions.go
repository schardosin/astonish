package migration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/schardosin/astonish/pkg/store"
	adksession "google.golang.org/adk/session"
)

// sessionIndex matches the on-disk index.json format from pkg/session.
type sessionIndex struct {
	Version  int                   `json:"version"`
	Sessions map[string]sessionMeta `json:"sessions"`
}

// sessionMeta matches the on-disk SessionMeta from pkg/session.
type sessionMeta struct {
	ID           string    `json:"id"`
	AppName      string    `json:"appName"`
	UserID       string    `json:"userId"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	Title        string    `json:"title,omitempty"`
	MessageCount int       `json:"messageCount"`
	ParentID     string    `json:"parentId,omitempty"`
	FleetKey     string    `json:"fleetKey,omitempty"`
	FleetName    string    `json:"fleetName,omitempty"`
	IssueNumber  int       `json:"issueNumber,omitempty"`
	Repo         string    `json:"repo,omitempty"`
	WorkspaceDir string    `json:"workspaceDir,omitempty"`
}

// transcriptEntry matches the on-disk JSONL format from pkg/session/transcript.go.
type transcriptEntry struct {
	Type      string           `json:"type"`      // "header" or "event"
	SessionID string           `json:"sessionId,omitempty"`
	Version   int              `json:"version,omitempty"`
	Event     *adksession.Event `json:"event,omitempty"`
}

func (m *Migrator) migrateSessions(ctx context.Context, teamDS store.TeamDataStore) (int, error) {
	sessDir := filepath.Join(m.configDir, "sessions")
	indexPath := filepath.Join(sessDir, "index.json")

	// Check if session index exists
	if _, err := os.Stat(indexPath); os.IsNotExist(err) {
		m.emitProgress(CatSessions, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatSessions, 0, 0, "counting", "")

	// Parse the session index
	indexData, err := os.ReadFile(indexPath)
	if err != nil {
		m.emitProgress(CatSessions, 0, 0, "error", "cannot read index.json")
		return 0, fmt.Errorf("cannot read session index: %w", err)
	}

	var idx sessionIndex
	if err := json.Unmarshal(indexData, &idx); err != nil {
		m.emitProgress(CatSessions, 0, 0, "error", "invalid index.json format")
		return 0, fmt.Errorf("invalid session index: %w", err)
	}

	total := len(idx.Sessions)
	if total == 0 {
		m.emitProgress(CatSessions, 0, 0, "skipped", "")
		return 0, nil
	}

	m.emitProgress(CatSessions, 0, total, "migrating", "")

	sessStore := teamDS.Sessions()
	count := 0

	for _, sm := range idx.Sessions {
		if ctx.Err() != nil {
			return count, ctx.Err()
		}

		// Insert session metadata
		meta := store.SessionMeta{
			ID:           sm.ID,
			AppName:      sm.AppName,
			UserID:       m.userID, // Use the new platform user ID
			CreatedAt:    sm.CreatedAt,
			UpdatedAt:    sm.UpdatedAt,
			Title:        sm.Title,
			MessageCount: sm.MessageCount,
			ParentID:     sm.ParentID,
			FleetKey:     sm.FleetKey,
			FleetName:    sm.FleetName,
			WorkspaceDir: sm.WorkspaceDir,
		}
		if err := sessStore.AddSessionMeta(meta); err != nil {
			return count, fmt.Errorf("failed to insert session %s: %w", sm.ID, err)
		}

		// Read and insert transcript events
		transcriptPath := filepath.Join(sessDir, sm.AppName, sm.UserID, sm.ID+".jsonl")
		if err := m.migrateTranscript(ctx, sessStore, sm.ID, transcriptPath); err != nil {
			// Non-fatal: session meta is migrated, just no events
			fmt.Fprintf(os.Stderr, "warning: failed to migrate transcript for session %s: %v\n", sm.ID, err)
		}

		count++
		m.emitProgress(CatSessions, count, total, "migrating", "")
	}

	m.emitProgress(CatSessions, count, total, "done", "")
	return count, nil
}

// migrateTranscript reads a session's JSONL transcript file and inserts events into PG.
func (m *Migrator) migrateTranscript(ctx context.Context, sessStore store.SessionStore, sessionID, path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No transcript file is OK
		}
		return err
	}
	defer f.Close()

	// Create an in-memory session for the ADK AppendEvent interface
	inmem := adksession.InMemoryService()
	resp, err := inmem.Create(ctx, &adksession.CreateRequest{
		AppName:   "astonish",
		UserID:    m.userID,
		SessionID: sessionID,
	})
	if err != nil {
		return fmt.Errorf("failed to create in-memory session: %w", err)
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var entry transcriptEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue // Skip malformed lines
		}

		if entry.Type != "event" || entry.Event == nil {
			continue // Skip headers and non-event entries
		}

		// Insert event into PG via the session store
		if err := sessStore.AppendEvent(ctx, resp.Session, entry.Event); err != nil {
			// Log but don't fail on individual event errors
			continue
		}
	}

	return scanner.Err()
}
