package sqlitestore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/schardosin/astonish/pkg/apps"
	"github.com/schardosin/astonish/pkg/store"

	_ "modernc.org/sqlite"
)

// sqliteAppStateSQLStore implements store.AppStateSQLStore for SQLite.
//
// Each app gets its own .db file in {dataDir}/apps/{slug}.db.
// This mirrors the PostgreSQL approach (per-app schema) using per-app files.
// App SQL runs directly against the app's database — no translation needed.
type sqliteAppStateSQLStore struct {
	appsDir string // e.g., ~/.local/share/astonish/apps
	mu      sync.Mutex
	dbs     map[string]*sql.DB
}

// newSQLiteAppStateSQLStore creates a new app state SQL store.
func newSQLiteAppStateSQLStore(dataDir string) *sqliteAppStateSQLStore {
	return &sqliteAppStateSQLStore{
		appsDir: filepath.Join(dataDir, "apps"),
		dbs:     make(map[string]*sql.DB),
	}
}

// getDB returns (or lazily opens) the SQLite database for the given app slug.
func (s *sqliteAppStateSQLStore) getDB(appSlug string) (*sql.DB, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if db, ok := s.dbs[appSlug]; ok {
		if err := db.Ping(); err == nil {
			return db, nil
		}
		// Dead connection — close and reopen.
		db.Close()
		delete(s.dbs, appSlug)
	}

	if err := os.MkdirAll(s.appsDir, 0750); err != nil {
		return nil, fmt.Errorf("create apps dir: %w", err)
	}

	dbPath := filepath.Join(s.appsDir, appSlug+".db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open app database %q: %w", dbPath, err)
	}

	// Single connection — per-app databases have low concurrency.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		slog.Debug("app state SQL: failed to enable WAL", "app", appSlug, "error", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		slog.Debug("app state SQL: failed to set busy timeout", "app", appSlug, "error", err)
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		slog.Debug("app state SQL: failed to enable foreign keys", "app", appSlug, "error", err)
	}

	s.dbs[appSlug] = db
	slog.Debug("opened app state database", "app", appSlug, "path", dbPath)
	return db, nil
}

// EnsureSchema is a no-op for SQLite — the file is created on first open.
func (s *sqliteAppStateSQLStore) EnsureSchema(_ context.Context, _ string) error {
	return nil
}

// Query executes a read-only SQL statement against the app's database.
func (s *sqliteAppStateSQLStore) Query(ctx context.Context, appSlug, sqlStr string, params ...any) ([]map[string]any, error) {
	db, err := s.getDB(appSlug)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, sqlStr, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return sqlRowsToMaps(rows)
}

// Exec executes a write/DDL SQL statement against the app's database.
func (s *sqliteAppStateSQLStore) Exec(ctx context.Context, appSlug, sqlStr string, params ...any) (int64, int64, error) {
	db, err := s.getDB(appSlug)
	if err != nil {
		return 0, 0, err
	}

	result, err := db.ExecContext(ctx, sqlStr, params...)
	if err != nil {
		return 0, 0, err
	}

	rowsAffected, _ := result.RowsAffected()
	lastInsertID, _ := result.LastInsertId()
	return rowsAffected, lastInsertID, nil
}

// DropSchema closes and deletes the app's database file.
func (s *sqliteAppStateSQLStore) DropSchema(_ context.Context, appSlug string) error {
	s.mu.Lock()
	if db, ok := s.dbs[appSlug]; ok {
		db.Close()
		delete(s.dbs, appSlug)
	}
	s.mu.Unlock()

	var errs []string
	for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
		path := filepath.Join(s.appsDir, appSlug+suffix)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("%s: %v", suffix, err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("errors removing app db: %s", strings.Join(errs, "; "))
	}
	return nil
}

// DropSchemasWithPrefix removes all app databases whose slug starts with prefix.
// Used for cleaning up session-scoped app databases.
func (s *sqliteAppStateSQLStore) DropSchemasWithPrefix(_ context.Context, prefix string) error {
	entries, err := os.ReadDir(s.appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read apps dir: %w", err)
	}

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".db") {
			continue
		}
		// Skip journal files.
		if strings.HasSuffix(name, ".db-wal") || strings.HasSuffix(name, ".db-shm") {
			continue
		}

		slug := strings.TrimSuffix(name, ".db")
		if !strings.HasPrefix(slug, prefix) {
			continue
		}

		// Close pooled connection.
		s.mu.Lock()
		if db, ok := s.dbs[slug]; ok {
			db.Close()
			delete(s.dbs, slug)
		}
		s.mu.Unlock()

		// Remove .db and journal files.
		for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
			path := filepath.Join(s.appsDir, slug+suffix)
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				slog.Debug("drop schemas with prefix: failed to remove", "path", path, "error", err)
			}
		}
	}
	return nil
}

// CloseAll closes all open app database connections.
func (s *sqliteAppStateSQLStore) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for key, db := range s.dbs {
		if err := db.Close(); err != nil {
			slog.Debug("error closing app state database", "key", key, "error", err)
		}
	}
	s.dbs = make(map[string]*sql.DB)
}

// CleanupOrphanDBs removes .db files that have no matching app in the given
// store and are older than the given age threshold. Used by garbage collection.
func (s *sqliteAppStateSQLStore) CleanupOrphanDBs(ctx context.Context, appStore store.AppStore, maxAge fmt.Stringer) int {
	// Not used in the new architecture — cleanup is via DropSchema on delete.
	return 0
}

// SlugFromAppName converts an app name to a slug (delegates to apps.Slugify).
func SlugFromAppName(name string) string {
	return apps.Slugify(name)
}

// sqlRowsToMaps converts *sql.Rows into a slice of column-name-to-value maps.
func sqlRowsToMaps(rows *sql.Rows) ([]map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	var results []map[string]any
	for rows.Next() {
		values := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range values {
			ptrs[i] = &values[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, col := range cols {
			row[col] = values[i]
		}
		results = append(results, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if results == nil {
		results = []map[string]any{}
	}
	return results, nil
}

// Compile-time check.
var _ store.AppStateSQLStore = (*sqliteAppStateSQLStore)(nil)
