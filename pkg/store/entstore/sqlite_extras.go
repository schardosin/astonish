package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// applySQLiteExtras executes SQLite-specific post-DDL SQL that Ent's
// Schema.Create() cannot produce: FTS5 virtual tables and sync triggers.
//
// All statements are idempotent (IF NOT EXISTS) so this function is safe to
// call on every startup or provisioning.
//
// For PostgreSQL this is a no-op (PG uses tsvector triggers instead).
func (s *Store) applySQLiteExtras(ctx context.Context, scope Scope, db *sql.DB) error {
	if s.dialect != DialectSQLite {
		return nil
	}

	var stmts []string
	switch scope {
	case ScopePlatform:
		stmts = platformSQLiteExtras
	case ScopeOrg:
		stmts = orgSQLiteExtras
	case ScopeTeam:
		stmts = teamSQLiteExtras
	case ScopePersonal:
		stmts = personalSQLiteExtras
	default:
		return fmt.Errorf("unknown scope: %d", scope)
	}

	for i, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			slog.Warn("sqlite_extras: statement failed",
				"scope", scope, "index", i, "error", err,
				"sql_prefix", truncate(stmt, 80))
			return fmt.Errorf("sqlite_extras scope=%d stmt=%d: %w", scope, i, err)
		}
	}
	return nil
}

// =============================================================================
// Org SQLite extras (table: org_memories)
// =============================================================================

var orgSQLiteExtras = []string{
	// FTS5 virtual table in content-sync mode.
	// content=org_memories means FTS5 reads content from the main table.
	`CREATE VIRTUAL TABLE IF NOT EXISTS org_memories_fts USING fts5(
		chunk_text,
		content=org_memories,
		content_rowid=rowid
	)`,

	// Trigger: keep FTS5 in sync on INSERT.
	`CREATE TRIGGER IF NOT EXISTS trg_org_memories_fts_insert
		AFTER INSERT ON org_memories
	BEGIN
		INSERT INTO org_memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
	END`,

	// Trigger: keep FTS5 in sync on DELETE.
	`CREATE TRIGGER IF NOT EXISTS trg_org_memories_fts_delete
		AFTER DELETE ON org_memories
	BEGIN
		INSERT INTO org_memories_fts(org_memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
	END`,

	// Trigger: keep FTS5 in sync on UPDATE of chunk_text.
	`CREATE TRIGGER IF NOT EXISTS trg_org_memories_fts_update
		AFTER UPDATE OF chunk_text ON org_memories
	BEGIN
		INSERT INTO org_memories_fts(org_memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
		INSERT INTO org_memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
	END`,
}

// =============================================================================
// Team SQLite extras (table: memories)
// =============================================================================

var teamSQLiteExtras = []string{
	// FTS5 virtual table in content-sync mode.
	`CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
		chunk_text,
		content=memories,
		content_rowid=rowid
	)`,

	// Trigger: keep FTS5 in sync on INSERT.
	`CREATE TRIGGER IF NOT EXISTS trg_memories_fts_insert
		AFTER INSERT ON memories
	BEGIN
		INSERT INTO memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
	END`,

	// Trigger: keep FTS5 in sync on DELETE.
	`CREATE TRIGGER IF NOT EXISTS trg_memories_fts_delete
		AFTER DELETE ON memories
	BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
	END`,

	// Trigger: keep FTS5 in sync on UPDATE of chunk_text.
	`CREATE TRIGGER IF NOT EXISTS trg_memories_fts_update
		AFTER UPDATE OF chunk_text ON memories
	BEGIN
		INSERT INTO memories_fts(memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
		INSERT INTO memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
	END`,
}

// =============================================================================
// Personal SQLite extras (table: memories — same as team, separate DB file)
// =============================================================================

var personalSQLiteExtras = teamSQLiteExtras

// =============================================================================
// Platform SQLite extras (seed data: @base layer + template)
// =============================================================================

var platformSQLiteExtras = []string{
	// Seed the @base sandbox layer (raw OS image placeholder).
	`INSERT INTO sandbox_layers (layer_id, parent_layer, cephfs_path, size_bytes, ref_count)
	 VALUES ('@base', NULL, '/mnt/astonish-layers/@base', 0, 1)
	 ON CONFLICT (layer_id) DO NOTHING`,

	// Seed the global "base" sandbox template pointing to @base layer.
	`INSERT INTO sandbox_templates (id, slug, scope, owner_id, purpose, name, description, parent_template_id, top_layer_id, version)
	 VALUES (
	     'a0000000-0000-4000-8000-000000000001',
	     'base', 'global', '', '', 'Base',
	     'Deployment-wide base sandbox image',
	     NULL, '@base', 1
	 )
	 ON CONFLICT (scope, owner_id, slug) DO NOTHING`,
}
