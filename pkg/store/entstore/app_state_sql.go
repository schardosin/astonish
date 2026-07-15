package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/SAP/astonish/pkg/store"
)

// =============================================================================
// PostgreSQL implementation
// =============================================================================

// pgAppStateSQLStore implements store.AppStateSQLStore for PostgreSQL.
// Each app gets its own schema (e.g., team_general_app_todo_app) within the org DB.
type pgAppStateSQLStore struct {
	db         *sql.DB
	teamSchema string // e.g., "team_general"
}

var _ store.AppStateSQLStore = (*pgAppStateSQLStore)(nil)

// IsPGBackend is a marker method for dialect detection (pgDetector interface).
func (s *pgAppStateSQLStore) IsPGBackend() {}

func (s *pgAppStateSQLStore) appSchemaName(appSlug string) string {
	return s.teamSchema + "_app_" + appSlug
}

func (s *pgAppStateSQLStore) EnsureSchema(ctx context.Context, appSlug string) error {
	if err := validateSlug(appSlug); err != nil {
		return fmt.Errorf("ensure schema: %w", err)
	}
	schema := s.appSchemaName(appSlug)
	quoted := fmt.Sprintf(`"%s"`, strings.ReplaceAll(schema, `"`, `""`))
	_, err := s.db.ExecContext(ctx, "CREATE SCHEMA IF NOT EXISTS "+quoted) // CodeQL[go/sql-injection]: schema name is validated by validateSlug allowlist and properly quoted
	return err
}

func (s *pgAppStateSQLStore) Query(ctx context.Context, appSlug, sqlStr string, params ...any) ([]map[string]any, error) {
	if err := validateSlug(appSlug); err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	schema := s.appSchemaName(appSlug)

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()

	// Set search_path to the app schema.
	quoted := fmt.Sprintf(`"%s"`, strings.ReplaceAll(schema, `"`, `""`))
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted); err != nil { // CodeQL[go/sql-injection]: schema name is validated by validateSlug allowlist and properly quoted
		return nil, fmt.Errorf("set search_path: %w", err)
	}
	defer conn.ExecContext(ctx, "RESET search_path") //nolint:errcheck

	rows, err := conn.QueryContext(ctx, sqlStr, params...) // CodeQL[go/sql-injection]: sqlStr is intentionally caller-provided raw SQL for per-app sandbox execution
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
	return results, rows.Err()
}

func (s *pgAppStateSQLStore) Exec(ctx context.Context, appSlug, sqlStr string, params ...any) (int64, int64, error) {
	if err := validateSlug(appSlug); err != nil {
		return 0, 0, fmt.Errorf("exec: %w", err)
	}
	schema := s.appSchemaName(appSlug)

	// Ensure schema exists for DDL operations.
	if err := s.EnsureSchema(ctx, appSlug); err != nil {
		return 0, 0, fmt.Errorf("ensure schema: %w", err)
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Close()

	quoted := fmt.Sprintf(`"%s"`, strings.ReplaceAll(schema, `"`, `""`))
	if _, err := conn.ExecContext(ctx, "SET search_path TO "+quoted); err != nil { // CodeQL[go/sql-injection]: schema name is validated by validateSlug allowlist and properly quoted
		return 0, 0, fmt.Errorf("set search_path: %w", err)
	}
	defer conn.ExecContext(ctx, "RESET search_path") //nolint:errcheck

	// For INSERT statements, try to get lastInsertId via RETURNING.
	upperSQL := strings.TrimSpace(strings.ToUpper(sqlStr))
	if strings.HasPrefix(upperSQL, "INSERT") && !strings.Contains(upperSQL, "RETURNING") {
		sqlStr = sqlStr + " RETURNING id"
		var lastID int64
		err := conn.QueryRowContext(ctx, sqlStr, params...).Scan(&lastID) // CodeQL[go/sql-injection]: sqlStr is intentionally caller-provided raw SQL for per-app sandbox execution
		if err != nil {
			// If RETURNING id fails (no id column), fall back to regular exec.
			sqlStr = strings.TrimSuffix(sqlStr, " RETURNING id")
			res, execErr := conn.ExecContext(ctx, sqlStr, params...) // CodeQL[go/sql-injection]: sqlStr is intentionally caller-provided raw SQL for per-app sandbox execution
			if execErr != nil {
				return 0, 0, execErr
			}
			ra, _ := res.RowsAffected()
			return ra, 0, nil
		}
		return 1, lastID, nil
	}

	res, err := conn.ExecContext(ctx, sqlStr, params...) // CodeQL[go/sql-injection]: sqlStr is intentionally caller-provided raw SQL for per-app sandbox execution
	if err != nil {
		return 0, 0, err
	}
	ra, _ := res.RowsAffected()
	lid, _ := res.LastInsertId()
	return ra, lid, nil
}

func (s *pgAppStateSQLStore) DropSchema(ctx context.Context, appSlug string) error {
	if err := validateSlug(appSlug); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}
	schema := s.appSchemaName(appSlug)
	quoted := fmt.Sprintf(`"%s"`, strings.ReplaceAll(schema, `"`, `""`))
	_, err := s.db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE") // CodeQL[go/sql-injection]: schema name is validated by validateSlug allowlist and properly quoted
	return err
}

func (s *pgAppStateSQLStore) DropSchemasWithPrefix(ctx context.Context, prefix string) error {
	fullPrefix := s.teamSchema + "_app_" + prefix

	rows, err := s.db.QueryContext(ctx,
		`SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE $1`,
		fullPrefix+"%")
	if err != nil {
		return err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		schemas = append(schemas, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, schema := range schemas {
		quoted := fmt.Sprintf(`"%s"`, strings.ReplaceAll(schema, `"`, `""`))
		if _, err := s.db.ExecContext(ctx, "DROP SCHEMA IF EXISTS "+quoted+" CASCADE"); err != nil {
			return err
		}
	}
	return nil
}

// =============================================================================
// SQLite implementation
// =============================================================================

// sqliteAppStateSQLStore implements store.AppStateSQLStore for SQLite.
// Each app gets its own .db file in the apps directory.
type sqliteAppStateSQLStore struct {
	appsDir string
	mu      sync.Mutex
	dbs     map[string]*sql.DB
}

var _ store.AppStateSQLStore = (*sqliteAppStateSQLStore)(nil)

func newSQLiteAppStateSQLStore(dataDir string) *sqliteAppStateSQLStore {
	appsDir := filepath.Join(dataDir, "apps")
	return &sqliteAppStateSQLStore{
		appsDir: appsDir,
		dbs:     make(map[string]*sql.DB),
	}
}

func (s *sqliteAppStateSQLStore) openDB(appSlug string) (*sql.DB, error) {
	if err := validateSlug(appSlug); err != nil {
		return nil, fmt.Errorf("open app db: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if db, ok := s.dbs[appSlug]; ok {
		return db, nil
	}

	if err := os.MkdirAll(s.appsDir, 0750); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(s.appsDir, appSlug+".db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s.dbs[appSlug] = db
	return db, nil
}

func (s *sqliteAppStateSQLStore) EnsureSchema(_ context.Context, _ string) error {
	// SQLite: no-op, database file is created on first open.
	return nil
}

func (s *sqliteAppStateSQLStore) Query(ctx context.Context, appSlug, sqlStr string, params ...any) ([]map[string]any, error) {
	db, err := s.openDB(appSlug)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, sqlStr, params...) // CodeQL[go/sql-injection]: sqlStr is intentionally caller-provided raw SQL for per-app sandbox execution
	if err != nil {
		return nil, err
	}
	defer rows.Close()

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
	return results, rows.Err()
}

func (s *sqliteAppStateSQLStore) Exec(ctx context.Context, appSlug, sqlStr string, params ...any) (int64, int64, error) {
	db, err := s.openDB(appSlug)
	if err != nil {
		return 0, 0, err
	}

	res, err := db.ExecContext(ctx, sqlStr, params...) // CodeQL[go/sql-injection]: sqlStr is intentionally caller-provided raw SQL for per-app sandbox execution
	if err != nil {
		return 0, 0, err
	}
	ra, _ := res.RowsAffected()
	lid, _ := res.LastInsertId()
	return ra, lid, nil
}

func (s *sqliteAppStateSQLStore) DropSchema(_ context.Context, appSlug string) error {
	if err := validateSlug(appSlug); err != nil {
		return fmt.Errorf("drop schema: %w", err)
	}

	s.mu.Lock()
	if db, ok := s.dbs[appSlug]; ok {
		db.Close()
		delete(s.dbs, appSlug)
	}
	s.mu.Unlock()

	base := filepath.Join(s.appsDir, appSlug+".db")
	for _, suffix := range []string{"", "-wal", "-shm"} {
		os.Remove(base + suffix)
	}
	return nil
}

func (s *sqliteAppStateSQLStore) DropSchemasWithPrefix(_ context.Context, prefix string) error {
	entries, err := os.ReadDir(s.appsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".db") {
			continue
		}
		slug := strings.TrimSuffix(name, ".db")
		if strings.HasPrefix(slug, prefix) {
			s.mu.Lock()
			if db, ok := s.dbs[slug]; ok {
				db.Close()
				delete(s.dbs, slug)
			}
			s.mu.Unlock()

			base := filepath.Join(s.appsDir, slug+".db")
			for _, suffix := range []string{"", "-wal", "-shm"} {
				os.Remove(base + suffix)
			}
		}
	}
	return nil
}

// CloseAll closes all open SQLite app databases.
func (s *sqliteAppStateSQLStore) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for slug, db := range s.dbs {
		db.Close()
		delete(s.dbs, slug)
	}
}
