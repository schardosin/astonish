package sqlitestore

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
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
	migrationPlatform MigrationLevel = "platform"
	migrationOrg      MigrationLevel = "org"
	migrationTeam     MigrationLevel = "team"
	migrationPersonal MigrationLevel = "personal"
)

// migrate applies all pending migrations for the given level to the database.
// It creates a schema_migrations table to track applied versions.
// Migrations are applied in filename order and are idempotent.
func migrate(ctx context.Context, db *sql.DB, level MigrationLevel) error {
	// Ensure tracking table exists.
	_, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TEXT NOT NULL DEFAULT (datetime('now'))
		)
	`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := loadMigrations(level)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	applied, err := getApplied(ctx, db)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if applied[entry.Name] {
			continue
		}

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", entry.Name, err)
		}

		if _, err := tx.ExecContext(ctx, entry.SQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", entry.Name, err)
		}

		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version) VALUES (?)`, entry.Name); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", entry.Name, err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name, err)
		}
	}

	return nil
}

type migrationEntry struct {
	Name string
	SQL  string
}

func loadMigrations(level MigrationLevel) ([]migrationEntry, error) {
	var fsys embed.FS
	var dir string

	switch level {
	case migrationPlatform:
		fsys = platformMigrations
		dir = "migrations/platform"
	case migrationOrg:
		fsys = orgMigrations
		dir = "migrations/org"
	case migrationTeam:
		fsys = teamMigrations
		dir = "migrations/team"
	case migrationPersonal:
		fsys = personalMigrations
		dir = "migrations/personal"
	default:
		return nil, fmt.Errorf("unknown migration level: %s", level)
	}

	dirEntries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", dir, err)
	}

	var entries []migrationEntry
	for _, de := range dirEntries {
		if de.IsDir() || !strings.HasSuffix(de.Name(), ".sql") {
			continue
		}
		content, err := fs.ReadFile(fsys, path.Join(dir, de.Name()))
		if err != nil {
			return nil, fmt.Errorf("read migration %s: %w", de.Name(), err)
		}
		entries = append(entries, migrationEntry{
			Name: de.Name(),
			SQL:  string(content),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name < entries[j].Name
	})

	return entries, nil
}

func getApplied(ctx context.Context, db *sql.DB) (map[string]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}
