package api

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schardosin/astonish/pkg/apps"

	_ "modernc.org/sqlite"
)

// ── DB Connection Pool ───────────────────────────────────────────────

// appDBPool manages per-app SQLite database connections.
// Each app gets its own .db file in the apps directory.
var appDBPool = struct {
	mu sync.Mutex
	dbs map[string]*sql.DB
}{
	dbs: make(map[string]*sql.DB),
}

// getAppDB returns (or lazily opens) a SQLite database for the given app.
// Database file: ~/.config/astonish/apps/{slugified-name}.db
func getAppDB(appName string) (*sql.DB, error) {
	if appName == "" {
		return nil, fmt.Errorf("app name is required for state operations")
	}

	slug := apps.Slugify(appName)
	key := slug

	appDBPool.mu.Lock()
	defer appDBPool.mu.Unlock()

	if db, ok := appDBPool.dbs[key]; ok {
		// Verify connection is still alive
		if err := db.Ping(); err == nil {
			return db, nil
		}
		// Dead connection — close and reopen
		db.Close()
		delete(appDBPool.dbs, key)
	}

	dir, err := apps.AppsDir()
	if err != nil {
		return nil, fmt.Errorf("cannot determine apps dir: %w", err)
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("cannot create apps dir: %w", err)
	}

	dbPath := filepath.Join(dir, slug+".db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database %q: %w", dbPath, err)
	}

	// Enable WAL mode for better concurrent read/write performance
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		slog.Debug("failed to enable WAL mode", "app", appName, "error", err)
	}
	// Enable foreign keys
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		slog.Debug("failed to enable foreign keys", "app", appName, "error", err)
	}

	appDBPool.dbs[key] = db
	slog.Debug("opened app database", "app", appName, "path", dbPath)
	return db, nil
}

// CloseAllAppDBs closes all open app database connections.
// Call this on server shutdown.
func CloseAllAppDBs() {
	appDBPool.mu.Lock()
	defer appDBPool.mu.Unlock()

	for key, db := range appDBPool.dbs {
		if err := db.Close(); err != nil {
			slog.Debug("error closing app database", "key", key, "error", err)
		}
	}
	appDBPool.dbs = make(map[string]*sql.DB)
}

// ── Request/Response Types ───────────────────────────────────────────

type appStateRequest struct {
	AppName   string `json:"appName"`
	SQL       string `json:"sql"`
	Params    []any  `json:"params"`
	RequestID string `json:"requestId"`
}

type appStateResponse struct {
	RequestID string `json:"requestId"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

// ── Handlers ─────────────────────────────────────────────────────────

// AppStateQueryHandler handles read-only SQL queries against an app's database.
// Only SELECT, PRAGMA, and EXPLAIN statements are allowed.
//
// POST /api/apps/state/query
func AppStateQueryHandler(w http.ResponseWriter, r *http.Request) {
	var req appStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.AppName == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "appName is required",
		})
		return
	}

	if req.SQL == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "sql is required",
		})
		return
	}

	// Validate: only read-only statements allowed
	trimmed := strings.TrimSpace(strings.ToUpper(req.SQL))
	if !strings.HasPrefix(trimmed, "SELECT") &&
		!strings.HasPrefix(trimmed, "PRAGMA") &&
		!strings.HasPrefix(trimmed, "EXPLAIN") {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "only SELECT, PRAGMA, and EXPLAIN statements are allowed on the query endpoint",
		})
		return
	}

	slog.Debug("app state query", "app", req.AppName, "requestId", req.RequestID, "sql", truncateSQL(req.SQL))

	db, err := getAppDB(req.AppName)
	if err != nil {
		respondJSON(w, http.StatusOK, appStateResponse{
			RequestID: req.RequestID,
			Error:     err.Error(),
		})
		return
	}

	rows, err := db.QueryContext(r.Context(), req.SQL, req.Params...) //nolint:gosec // SQL is user-provided but restricted to read-only via allowlist above
	if err != nil {
		respondJSON(w, http.StatusOK, appStateResponse{
			RequestID: req.RequestID,
			Error:     fmt.Sprintf("query error: %v", err),
		})
		return
	}
	defer rows.Close()

	results, err := rowsToMaps(rows)
	if err != nil {
		respondJSON(w, http.StatusOK, appStateResponse{
			RequestID: req.RequestID,
			Error:     fmt.Sprintf("failed to read results: %v", err),
		})
		return
	}

	respondJSON(w, http.StatusOK, appStateResponse{
		RequestID: req.RequestID,
		Data:      results,
	})
}

// AppStateExecHandler handles write/DDL SQL statements against an app's database.
// CREATE, INSERT, UPDATE, DELETE, ALTER, DROP are all allowed.
//
// POST /api/apps/state/exec
func AppStateExecHandler(w http.ResponseWriter, r *http.Request) {
	var req appStateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			Error: "invalid request body",
		})
		return
	}

	if req.AppName == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "appName is required",
		})
		return
	}

	if req.SQL == "" {
		respondJSON(w, http.StatusBadRequest, appStateResponse{
			RequestID: req.RequestID,
			Error:     "sql is required",
		})
		return
	}

	slog.Debug("app state exec", "app", req.AppName, "requestId", req.RequestID, "sql", truncateSQL(req.SQL))

	db, err := getAppDB(req.AppName)
	if err != nil {
		respondJSON(w, http.StatusOK, appStateResponse{
			RequestID: req.RequestID,
			Error:     err.Error(),
		})
		return
	}

	result, err := db.ExecContext(r.Context(), req.SQL, req.Params...) //nolint:gosec // SQL is user-provided; params are parameterized, app-scoped DB
	if err != nil {
		respondJSON(w, http.StatusOK, appStateResponse{
			RequestID: req.RequestID,
			Error:     fmt.Sprintf("exec error: %v", err),
		})
		return
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()

	respondJSON(w, http.StatusOK, appStateResponse{
		RequestID: req.RequestID,
		Data: map[string]any{
			"rowsAffected": rowsAffected,
			"lastInsertId": lastInsertID,
		},
	})
}

// ── Helpers ──────────────────────────────────────────────────────────

// rowsToMaps converts sql.Rows into a slice of maps (column name → value).
func rowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(columns))
		pointers := make([]any, len(columns))
		for i := range values {
			pointers[i] = &values[i]
		}

		if err := rows.Scan(pointers...); err != nil {
			return nil, err
		}

		row := make(map[string]any, len(columns))
		for i, col := range columns {
			val := values[i]
			// Convert []byte to string for JSON compatibility
			if b, ok := val.([]byte); ok {
				row[col] = string(b)
			} else {
				row[col] = val
			}
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null when no rows
	if results == nil {
		results = []map[string]any{}
	}

	return results, nil
}

// truncateSQL truncates a SQL string for logging.
func truncateSQL(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 100 {
		return s[:100] + "..."
	}
	return s
}
