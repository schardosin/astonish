package pgstore

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/platform/*.sql
var platformMigrations embed.FS

//go:embed migrations/org/*.sql
var orgMigrations embed.FS

//go:embed migrations/team/*.sql
var teamMigrations embed.FS

//go:embed migrations/personal/*.sql
var personalMigrations embed.FS

// MigrationLevel identifies which set of migrations to apply.
type MigrationLevel string

const (
	MigrationPlatform MigrationLevel = "platform"
	MigrationOrg      MigrationLevel = "org"
	MigrationTeam     MigrationLevel = "team"
	MigrationPersonal MigrationLevel = "personal"
)

// migrationEntry holds a single migration file's name and content.
type migrationEntry struct {
	Name string
	SQL  string
}

// Migrate applies all pending migrations for the given level to the database.
//
// For platform and org levels, migrations are applied to the current database's
// public schema. For team and personal levels, the targetSchema parameter
// specifies which schema to apply migrations to (e.g., "team_engineering" or
// "personal_abc123").
//
// The function creates a schema_migrations table (in the target schema) to
// track which migrations have been applied. Migrations are applied in order
// and are idempotent (already-applied migrations are skipped).
func Migrate(ctx context.Context, conn *pgx.Conn, level MigrationLevel, targetSchema string) error {
	// Determine the schema for the migrations tracking table
	schema := "public"
	if targetSchema != "" {
		schema = targetSchema
	}

	// Ensure the schema_migrations table exists
	createTable := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS %s.schema_migrations (
			version  TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`, pgx.Identifier{schema}.Sanitize())

	if _, err := conn.Exec(ctx, createTable); err != nil {
		return fmt.Errorf("failed to create schema_migrations table in %s: %w", schema, err)
	}

	// Load migration files for this level
	entries, err := loadMigrations(level)
	if err != nil {
		return fmt.Errorf("failed to load migrations for %s: %w", level, err)
	}

	if len(entries) == 0 {
		return nil
	}

	// Get already-applied migrations
	applied, err := getAppliedMigrations(ctx, conn, schema)
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Apply pending migrations in order
	for _, entry := range entries {
		if applied[entry.Name] {
			continue
		}

		// For team/personal migrations, replace the schema placeholder
		sql := entry.SQL
		if targetSchema != "" {
			sql = strings.ReplaceAll(sql, "{{schema}}", pgx.Identifier{targetSchema}.Sanitize())
		}

		// Execute in a transaction
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("failed to begin transaction for migration %s: %w", entry.Name, err)
		}

		if _, err := tx.Exec(ctx, sql); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("migration %s failed: %w", entry.Name, err)
		}

		// Record the migration
		recordSQL := fmt.Sprintf(
			`INSERT INTO %s.schema_migrations (version) VALUES ($1)`,
			pgx.Identifier{schema}.Sanitize(),
		)
		if _, err := tx.Exec(ctx, recordSQL, entry.Name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("failed to record migration %s: %w", entry.Name, err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("failed to commit migration %s: %w", entry.Name, err)
		}
	}

	return nil
}

// loadMigrations reads and sorts migration files from the embedded filesystem.
func loadMigrations(level MigrationLevel) ([]migrationEntry, error) {
	var fsys embed.FS
	var dir string

	switch level {
	case MigrationPlatform:
		fsys = platformMigrations
		dir = "migrations/platform"
	case MigrationOrg:
		fsys = orgMigrations
		dir = "migrations/org"
	case MigrationTeam:
		fsys = teamMigrations
		dir = "migrations/team"
	case MigrationPersonal:
		fsys = personalMigrations
		dir = "migrations/personal"
	default:
		return nil, fmt.Errorf("unknown migration level: %s", level)
	}

	dirEntries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read migrations directory %s: %w", dir, err)
	}

	var entries []migrationEntry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".sql") {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, de.Name()))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", de.Name(), err)
		}
		entries = append(entries, migrationEntry{
			Name: de.Name(),
			SQL:  string(content),
		})
	}

	// Sort by filename (numeric prefix ensures correct order)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

// getAppliedMigrations returns a set of already-applied migration versions.
func getAppliedMigrations(ctx context.Context, conn *pgx.Conn, schema string) (map[string]bool, error) {
	query := fmt.Sprintf(
		`SELECT version FROM %s.schema_migrations`,
		pgx.Identifier{schema}.Sanitize(),
	)

	rows, err := conn.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}

	return applied, rows.Err()
}
