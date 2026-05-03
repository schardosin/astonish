package pgstore

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/schardosin/astonish/pkg/store"
)

// pgAppStateSQLStore implements store.AppStateSQLStore for PostgreSQL.
//
// Each app gets its own schema within the org database. The schema name
// follows the pattern: {teamSchema}_app_{appSlug}
// For example: team_general_app_todo_app
//
// SQL from the app is translated from SQLite dialect to PostgreSQL dialect
// (parameter placeholders, AUTOINCREMENT, etc.) before execution.
type pgAppStateSQLStore struct {
	pool       *pgxpool.Pool
	teamSchema string // e.g., "team_general"
}

// appSchemaName returns the PG schema name for the given app.
func (s *pgAppStateSQLStore) appSchemaName(appSlug string) string {
	return s.teamSchema + "_app_" + appSlug
}

func (s *pgAppStateSQLStore) EnsureSchema(ctx context.Context, appSlug string) error {
	schema := s.appSchemaName(appSlug)
	// Validate schema name to prevent injection (should only contain alphanums and underscores)
	if !isValidSchemaName(schema) {
		return fmt.Errorf("invalid app schema name: %q", schema)
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`CREATE SCHEMA IF NOT EXISTS %s`, pgx.Identifier{schema}.Sanitize()))
	return err
}

func (s *pgAppStateSQLStore) Query(ctx context.Context, appSlug, sql string, params ...any) ([]map[string]any, error) {
	schema := s.appSchemaName(appSlug)

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Set search_path so table names resolve within the app's schema
	if _, err := conn.Exec(ctx, fmt.Sprintf(`SET search_path TO %s`, pgx.Identifier{schema}.Sanitize())); err != nil {
		return nil, fmt.Errorf("failed to set search_path: %w", err)
	}

	rows, err := conn.Query(ctx, sql, params...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return pgxRowsToMaps(rows)
}

func (s *pgAppStateSQLStore) Exec(ctx context.Context, appSlug, sql string, params ...any) (int64, int64, error) {
	schema := s.appSchemaName(appSlug)

	// Ensure schema exists on any write/DDL operation
	if err := s.EnsureSchema(ctx, appSlug); err != nil {
		return 0, 0, fmt.Errorf("failed to ensure schema: %w", err)
	}

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to acquire connection: %w", err)
	}
	defer conn.Release()

	// Set search_path so table names resolve within the app's schema
	if _, err := conn.Exec(ctx, fmt.Sprintf(`SET search_path TO %s`, pgx.Identifier{schema}.Sanitize())); err != nil {
		return 0, 0, fmt.Errorf("failed to set search_path: %w", err)
	}

	// For INSERT statements, try to get the last inserted ID via RETURNING
	trimmed := strings.TrimSpace(strings.ToUpper(sql))
	if strings.HasPrefix(trimmed, "INSERT") && !strings.Contains(strings.ToUpper(sql), "RETURNING") {
		// Try appending RETURNING id — if the table has an `id` column this gives us lastInsertId
		returningSQL := sql + " RETURNING id"
		var lastID int64
		err := conn.QueryRow(ctx, returningSQL, params...).Scan(&lastID)
		if err == nil {
			return 1, lastID, nil
		}
		// If RETURNING id failed (no id column), fall through to regular exec
		slog.Debug("INSERT RETURNING id failed, falling back to regular exec", "error", err)
	}

	tag, err := conn.Exec(ctx, sql, params...)
	if err != nil {
		return 0, 0, err
	}

	return tag.RowsAffected(), 0, nil
}

func (s *pgAppStateSQLStore) DropSchema(ctx context.Context, appSlug string) error {
	schema := s.appSchemaName(appSlug)
	if !isValidSchemaName(schema) {
		return fmt.Errorf("invalid app schema name: %q", schema)
	}
	_, err := s.pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, pgx.Identifier{schema}.Sanitize()))
	return err
}

func (s *pgAppStateSQLStore) DropSchemasWithPrefix(ctx context.Context, prefix string) error {
	fullPrefix := s.teamSchema + "_app_" + prefix

	rows, err := s.pool.Query(ctx,
		`SELECT schema_name FROM information_schema.schemata WHERE schema_name LIKE $1`,
		fullPrefix+"%",
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		schemas = append(schemas, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for _, schema := range schemas {
		if _, err := s.pool.Exec(ctx, fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, pgx.Identifier{schema}.Sanitize())); err != nil {
			slog.Warn("failed to drop app schema", "schema", schema, "error", err)
		}
	}
	return nil
}

// --- helpers ---

// pgxRowsToMaps converts pgx.Rows into a slice of column-name-to-value maps.
func pgxRowsToMaps(rows pgx.Rows) ([]map[string]any, error) {
	descs := rows.FieldDescriptions()
	var results []map[string]any

	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, err
		}
		row := make(map[string]any, len(descs))
		for i, desc := range descs {
			row[string(desc.Name)] = values[i]
		}
		results = append(results, row)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Return empty array instead of null
	if results == nil {
		results = []map[string]any{}
	}
	return results, nil
}

// isValidSchemaName validates that a schema name contains only safe characters.
func isValidSchemaName(name string) bool {
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return len(name) > 0 && len(name) <= 128
}

// Compile-time check that pgAppStateSQLStore implements store.AppStateSQLStore.
var _ store.AppStateSQLStore = (*pgAppStateSQLStore)(nil)
