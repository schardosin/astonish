package entstore

import (
	"context"
	"database/sql"
	"log/slog"
)

// migrateOrgLegacySchema detects and fixes schema differences between
// tables created by the old pgstore SQL migrations and what Ent expects.
// All operations are idempotent — safe to run on every org DB open.
//
// Key difference: old pgstore created team_memberships with a composite
// PRIMARY KEY (user_id, team_id) and no `id` column. Ent expects a UUID
// `id` column as the primary key, with a separate unique index on
// (user_id, team_id).
func migrateOrgLegacySchema(ctx context.Context, db *sql.DB) {
	migrateTeamMemberships(ctx, db)
}

// migrateTeamMemberships adds the `id` UUID column and restructures the
// primary key if the table uses the old composite PK layout.
func migrateTeamMemberships(ctx context.Context, db *sql.DB) {
	// Check if the table exists at all.
	var exists bool
	err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = 'public' AND table_name = 'team_memberships'
		)
	`).Scan(&exists)
	if err != nil || !exists {
		return // Table doesn't exist — Schema.Create will handle it.
	}

	// Check if the `id` column already exists.
	var hasID bool
	err = db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'team_memberships'
			  AND column_name = 'id'
		)
	`).Scan(&hasID)
	if err != nil || hasID {
		return // Already migrated or error checking — skip.
	}

	slog.Info("migrating legacy team_memberships table: adding id column and restructuring PK")

	// Run the migration in a transaction.
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		slog.Warn("team_memberships migration: failed to begin tx", "error", err)
		return
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	stmts := []string{
		// 1. Add the UUID id column with auto-generated default.
		`ALTER TABLE team_memberships ADD COLUMN id UUID DEFAULT gen_random_uuid()`,

		// 2. Backfill id for existing rows (DEFAULT handles this, but be explicit).
		`UPDATE team_memberships SET id = gen_random_uuid() WHERE id IS NULL`,

		// 3. Set NOT NULL on id.
		`ALTER TABLE team_memberships ALTER COLUMN id SET NOT NULL`,

		// 4. Drop the old composite primary key.
		`ALTER TABLE team_memberships DROP CONSTRAINT IF EXISTS team_memberships_pkey`,

		// 5. Set id as the new primary key.
		`ALTER TABLE team_memberships ADD PRIMARY KEY (id)`,

		// 6. Create the unique index Ent expects on (user_id, team_id).
		// The old composite PK enforced this uniqueness; now we need an explicit index.
		`CREATE UNIQUE INDEX IF NOT EXISTS teammembership_user_id_team_id
		 ON team_memberships (user_id, team_id)`,

		// 7. Create individual indexes Ent expects.
		`CREATE INDEX IF NOT EXISTS teammembership_team_id
		 ON team_memberships (team_id)`,
		`CREATE INDEX IF NOT EXISTS teammembership_user_id
		 ON team_memberships (user_id)`,

		// 8. Drop the old CHECK constraint on role (Ent uses app-level validation).
		// The constraint name varies, so use a dynamic approach.
		`DO $$
		DECLARE
			r RECORD;
		BEGIN
			FOR r IN (
				SELECT conname FROM pg_constraint
				WHERE conrelid = 'team_memberships'::regclass
				  AND contype = 'c'
				  AND pg_get_constraintdef(oid) LIKE '%role%'
			) LOOP
				EXECUTE format('ALTER TABLE team_memberships DROP CONSTRAINT %I', r.conname);
			END LOOP;
		END $$`,
	}

	for i, stmt := range stmts {
		if _, err = tx.ExecContext(ctx, stmt); err != nil {
			slog.Warn("team_memberships migration: statement failed",
				"step", i+1, "error", err)
			return
		}
	}

	if err = tx.Commit(); err != nil {
		slog.Warn("team_memberships migration: commit failed", "error", err)
		return
	}

	slog.Info("team_memberships table successfully migrated to Ent schema")
	// Clear err so defer doesn't rollback.
	err = nil
}
