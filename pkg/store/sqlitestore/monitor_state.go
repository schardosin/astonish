package sqlitestore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/schardosin/astonish/pkg/fleet"
)

// SQLiteMonitorStateStore implements fleet.MonitorStateStore backed by SQLite.
// State is stored in the org's database under the team's scope.
type SQLiteMonitorStateStore struct {
	db *sql.DB
}

// NewSQLiteMonitorStateStore creates a DB-backed monitor state store.
// The db should be the team-scoped database (or org database for simpler setups).
func NewSQLiteMonitorStateStore(db *sql.DB) *SQLiteMonitorStateStore {
	return &SQLiteMonitorStateStore{db: db}
}

func (s *SQLiteMonitorStateStore) LoadState(planKey string) (*fleet.GitHubMonitorState, error) {
	var stateJSON []byte
	err := s.db.QueryRowContext(context.Background(),
		`SELECT state FROM fleet_monitor_state WHERE plan_key = ?`, planKey,
	).Scan(&stateJSON)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("load monitor state: %w", err)
	}

	var state fleet.GitHubMonitorState
	if err := json.Unmarshal(stateJSON, &state); err != nil {
		return nil, fmt.Errorf("unmarshal monitor state: %w", err)
	}
	return &state, nil
}

func (s *SQLiteMonitorStateStore) SaveState(planKey string, state *fleet.GitHubMonitorState) error {
	stateJSON, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("marshal monitor state: %w", err)
	}

	_, err = s.db.ExecContext(context.Background(),
		`INSERT INTO fleet_monitor_state (plan_key, state, updated_at)
		 VALUES (?, ?, datetime('now'))
		 ON CONFLICT(plan_key) DO UPDATE
		 SET state = excluded.state,
		     updated_at = datetime('now')`,
		planKey, string(stateJSON),
	)
	if err != nil {
		return fmt.Errorf("save monitor state: %w", err)
	}
	return nil
}

func (s *SQLiteMonitorStateStore) DeleteState(planKey string) error {
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM fleet_monitor_state WHERE plan_key = ?`, planKey)
	return err
}

// newMonitorStateStoreForTeam creates a monitor state store for the given org+team.
// It opens the team database and returns a store backed by it.
func (s *SQLiteStore) newMonitorStateStoreForTeam(orgSlug, teamSlug string) fleet.MonitorStateStore {
	teamDBPath := filepath.Join(s.dataDir, "orgs", orgSlug, "teams", teamSlug+".db")
	db, err := openDB(teamDBPath)
	if err != nil {
		// Fallback: use platform DB (won't isolate per-team but won't crash)
		return NewSQLiteMonitorStateStore(s.platformDB)
	}
	// Note: This opens a new connection. For frequent access, we should cache.
	// For fleet monitors (called every few minutes), this is acceptable.
	return NewSQLiteMonitorStateStore(db)
}

// Compile-time interface check
var _ fleet.MonitorStateStore = (*SQLiteMonitorStateStore)(nil)
