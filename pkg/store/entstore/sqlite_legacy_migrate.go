package entstore

import (
	"database/sql"
	"log/slog"
)

// migrateOrgSQLiteLegacy detects and fixes schema differences between
// tables created by the old sqlitestore SQL migrations and what Ent expects.
// This runs BEFORE Schema.Create so that Ent's migration (which copies rows
// to new temp tables) succeeds without NOT NULL constraint violations.
//
// All operations are idempotent — safe to run on every org DB open.
func migrateOrgSQLiteLegacy(db *sql.DB) {
	migrateTeamsTableSQLite(db)
	migrateTeamMembershipsSQLite(db)
	migrateOrgEncryptionKeysSQLite(db)
}

// migrateTeamsTableSQLite adds the schema_name column to the teams table
// and backfills it from the slug. Ent's Schema.Create requires this column
// to be NOT NULL, but old sqlitestore tables didn't have it.
func migrateTeamsTableSQLite(db *sql.DB) {
	// Check if teams table exists.
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='teams'`).Scan(&count)
	if err != nil || count == 0 {
		return // No teams table — Schema.Create will handle it.
	}

	// Check if schema_name column already exists.
	rows, err := db.Query(`PRAGMA table_info(teams)`)
	if err != nil {
		return
	}
	defer rows.Close()

	hasSchemaName := false
	for rows.Next() {
		var cid int
		var name, typeName string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dflt, &pk); err != nil {
			continue
		}
		if name == "schema_name" {
			hasSchemaName = true
			break
		}
	}

	if hasSchemaName {
		return // Already has the column.
	}

	slog.Info("SQLite org migration: adding schema_name column to teams table")

	// Add column (SQLite ADD COLUMN doesn't support NOT NULL without default).
	_, err = db.Exec(`ALTER TABLE teams ADD COLUMN schema_name TEXT DEFAULT ''`)
	if err != nil {
		slog.Warn("SQLite org migration: failed to add schema_name column", "error", err)
		return
	}

	// Backfill: schema_name = 'team_' || slug (matching teamSchemaName() logic).
	_, err = db.Exec(`UPDATE teams SET schema_name = 'team_' || slug WHERE schema_name = '' OR schema_name IS NULL`)
	if err != nil {
		slog.Warn("SQLite org migration: failed to backfill schema_name", "error", err)
		return
	}

	slog.Info("SQLite org migration: teams.schema_name column added and backfilled")
}

// migrateTeamMembershipsSQLite adds the id column to team_memberships
// if it uses the old composite PK layout (same issue as PostgreSQL).
func migrateTeamMembershipsSQLite(db *sql.DB) {
	// Check if team_memberships table exists.
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='team_memberships'`).Scan(&count)
	if err != nil || count == 0 {
		return
	}

	// Check if id column exists.
	rows, err := db.Query(`PRAGMA table_info(team_memberships)`)
	if err != nil {
		return
	}
	defer rows.Close()

	hasID := false
	for rows.Next() {
		var cid int
		var name, typeName string
		var notNull, pk int
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &dflt, &pk); err != nil {
			continue
		}
		if name == "id" {
			hasID = true
			break
		}
	}

	if hasID {
		return // Already has the id column.
	}

	slog.Info("SQLite org migration: restructuring team_memberships table (adding id column)")

	// SQLite doesn't support DROP PRIMARY KEY or ADD COLUMN with PRIMARY KEY.
	// We need to recreate the table. Use the standard SQLite pattern:
	// 1. Rename old table
	// 2. Create new table with desired schema
	// 3. Copy data
	// 4. Drop old table

	tx, err := db.Begin()
	if err != nil {
		slog.Warn("SQLite org migration: failed to begin tx for team_memberships", "error", err)
		return
	}

	stmts := []string{
		`ALTER TABLE team_memberships RENAME TO _team_memberships_old`,

		`CREATE TABLE team_memberships (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6)))),
			user_id TEXT NOT NULL,
			team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
			role TEXT NOT NULL DEFAULT 'member',
			joined_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(user_id, team_id)
		)`,

		`INSERT INTO team_memberships (user_id, team_id, role, joined_at)
		 SELECT user_id, team_id, role, joined_at FROM _team_memberships_old`,

		`DROP TABLE _team_memberships_old`,

		`CREATE INDEX IF NOT EXISTS teammembership_team_id ON team_memberships(team_id)`,
		`CREATE INDEX IF NOT EXISTS teammembership_user_id ON team_memberships(user_id)`,
	}

	for _, stmt := range stmts {
		if _, err := tx.Exec(stmt); err != nil {
			slog.Warn("SQLite org migration: team_memberships migration failed", "error", err)
			_ = tx.Rollback()
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Warn("SQLite org migration: team_memberships commit failed", "error", err)
		return
	}

	slog.Info("SQLite org migration: team_memberships table restructured with id column")
}

// migrateOrgEncryptionKeysSQLite ensures existing rows in org_encryption_keys
// have a valid id value. Old sqlitestore might have left id as NULL or empty
// if the row was inserted without specifying it.
func migrateOrgEncryptionKeysSQLite(db *sql.DB) {
	// Check if table exists.
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='org_encryption_keys'`).Scan(&count)
	if err != nil || count == 0 {
		return
	}

	// Backfill any rows with empty/NULL id.
	_, _ = db.Exec(`UPDATE org_encryption_keys SET id = lower(hex(randomblob(4)) || '-' || hex(randomblob(2)) || '-4' || substr(hex(randomblob(2)),2) || '-' || substr('89ab', abs(random()) % 4 + 1, 1) || substr(hex(randomblob(2)),2) || '-' || hex(randomblob(6))) WHERE id IS NULL OR id = ''`)
}
