package entstore

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

// Scope identifies the migration level for PG extras.
type Scope int

const (
	ScopePlatform Scope = iota
	ScopeOrg
	ScopeTeam
	ScopePersonal
)

// applyPGExtras executes PG-specific post-DDL SQL that Ent's Schema.Create()
// cannot produce: extensions, specialized indexes (HNSW, IVFFlat, GIN, partial),
// PL/pgSQL trigger functions, RLS policies, and seed data.
//
// All statements are idempotent (IF NOT EXISTS / CREATE OR REPLACE / ON CONFLICT
// DO NOTHING) so this function is safe to call on every startup or provisioning.
//
// For SQLite this is a no-op.
func (s *Store) applyPGExtras(ctx context.Context, scope Scope, db *sql.DB) error {
	if s.dialect != DialectPostgres {
		return nil
	}

	var stmts []string
	switch scope {
	case ScopePlatform:
		stmts = platformExtras
	case ScopeOrg:
		stmts = orgExtras
	case ScopeTeam:
		stmts = teamExtras
	case ScopePersonal:
		stmts = personalExtras
	default:
		return fmt.Errorf("unknown scope: %d", scope)
	}

	for i, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			slog.Warn("pg_extras: statement failed",
				"scope", scope, "index", i, "error", err,
				"sql_prefix", truncate(stmt, 80))
			return fmt.Errorf("pg_extras scope=%d stmt=%d: %w", scope, i, err)
		}
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// =============================================================================
// Platform extras
// =============================================================================

var platformExtras = []string{
	// --- Extension ---
	`CREATE EXTENSION IF NOT EXISTS vector`,

	// --- Specialized indexes ---
	`CREATE INDEX IF NOT EXISTS idx_tool_index_embedding
		ON tool_index USING hnsw (embedding vector_cosine_ops)`,

	`CREATE INDEX IF NOT EXISTS idx_users_oidc
		ON users(oidc_issuer, oidc_subject) WHERE oidc_issuer IS NOT NULL`,

	`CREATE INDEX IF NOT EXISTS idx_sandbox_layers_unreferenced
		ON sandbox_layers(added_at) WHERE ref_count = 0`,

	`CREATE INDEX IF NOT EXISTS idx_sandbox_layers_parent
		ON sandbox_layers(parent_layer) WHERE parent_layer IS NOT NULL`,

	`CREATE INDEX IF NOT EXISTS idx_sandbox_templates_parent
		ON sandbox_templates(parent_template_id) WHERE parent_template_id IS NOT NULL`,

	`CREATE INDEX IF NOT EXISTS idx_sandbox_templates_top_layer
		ON sandbox_templates(top_layer_id) WHERE top_layer_id IS NOT NULL`,

	// --- Cycle-detection trigger for sandbox_templates ---
	`CREATE OR REPLACE FUNCTION sandbox_templates_check_no_cycle() RETURNS trigger AS $$
DECLARE
    current_id UUID;
    depth      INTEGER := 0;
BEGIN
    IF NEW.parent_template_id IS NULL THEN
        RETURN NEW;
    END IF;
    current_id := NEW.parent_template_id;
    WHILE current_id IS NOT NULL LOOP
        IF current_id = NEW.id THEN
            RAISE EXCEPTION 'cycle detected in sandbox_templates parent chain at template %', NEW.id;
        END IF;
        depth := depth + 1;
        IF depth > 100 THEN
            RAISE EXCEPTION 'sandbox_templates parent chain exceeds depth 100 (possible cycle)';
        END IF;
        SELECT parent_template_id INTO current_id FROM sandbox_templates WHERE id = current_id;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql`,

	`DROP TRIGGER IF EXISTS trg_sandbox_templates_no_cycle ON sandbox_templates`,
	`CREATE TRIGGER trg_sandbox_templates_no_cycle
		BEFORE INSERT OR UPDATE OF parent_template_id ON sandbox_templates
		FOR EACH ROW EXECUTE FUNCTION sandbox_templates_check_no_cycle()`,

	// --- Ref-count helper function ---
	`CREATE OR REPLACE FUNCTION sandbox_layers_bump_ref(layer TEXT, delta INTEGER) RETURNS void AS $$
BEGIN
    IF layer IS NULL THEN
        RETURN;
    END IF;
    UPDATE sandbox_layers
       SET ref_count       = ref_count + delta,
           last_referenced = CASE WHEN delta > 0 THEN now() ELSE last_referenced END
     WHERE layer_id = layer;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'sandbox_layers_bump_ref: layer % not found', layer;
    END IF;
END;
$$ LANGUAGE plpgsql`,

	// --- Ref-count backstop trigger ---
	`CREATE OR REPLACE FUNCTION sandbox_templates_ref_count_backstop() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        PERFORM sandbox_layers_bump_ref(NEW.top_layer_id, 1);
        RETURN NEW;
    ELSIF TG_OP = 'UPDATE' THEN
        IF NEW.top_layer_id IS DISTINCT FROM OLD.top_layer_id THEN
            PERFORM sandbox_layers_bump_ref(OLD.top_layer_id, -1);
            PERFORM sandbox_layers_bump_ref(NEW.top_layer_id, 1);
        END IF;
        RETURN NEW;
    ELSIF TG_OP = 'DELETE' THEN
        PERFORM sandbox_layers_bump_ref(OLD.top_layer_id, -1);
        RETURN OLD;
    END IF;
    RETURN NULL;
END;
$$ LANGUAGE plpgsql`,

	`DROP TRIGGER IF EXISTS trg_sandbox_templates_ref_backstop ON sandbox_templates`,
	`CREATE TRIGGER trg_sandbox_templates_ref_backstop
		AFTER INSERT OR UPDATE OF top_layer_id OR DELETE ON sandbox_templates
		FOR EACH ROW EXECUTE FUNCTION sandbox_templates_ref_count_backstop()`,

	// Backstop trigger is DISABLED by default (application code manages ref counts).
	`ALTER TABLE sandbox_templates DISABLE TRIGGER trg_sandbox_templates_ref_backstop`,

	// --- Seed data: base layer + template ---
	`INSERT INTO sandbox_layers (layer_id, parent_layer, cephfs_path, size_bytes, ref_count)
	 VALUES ('@base', NULL, '/mnt/astonish-layers/@base', 0, 1)
	 ON CONFLICT (layer_id) DO NOTHING`,

	`INSERT INTO sandbox_templates (id, slug, scope, owner_id, purpose, name, description, parent_template_id, top_layer_id, version)
	 VALUES (
	     'a0000000-0000-4000-8000-000000000001',
	     'base', 'global', '', '', 'Base',
	     'Deployment-wide base sandbox image',
	     NULL, '@base', 1
	 )
	 ON CONFLICT (scope, owner_id, slug) DO NOTHING`,
}

// =============================================================================
// Org extras
// =============================================================================

var orgExtras = []string{
	// --- Extension ---
	`CREATE EXTENSION IF NOT EXISTS vector`,

	// --- Specialized indexes ---
	`CREATE INDEX IF NOT EXISTS idx_org_memories_embedding
		ON org_memories USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`,

	`CREATE INDEX IF NOT EXISTS idx_org_memories_tsv
		ON org_memories USING GIN (tsv)`,

	`CREATE INDEX IF NOT EXISTS idx_org_memories_session_id
		ON org_memories (session_id) WHERE session_id IS NOT NULL`,

	// --- tsvector trigger ---
	`CREATE OR REPLACE FUNCTION org_memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('english', NEW.chunk_text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql`,

	`DROP TRIGGER IF EXISTS trg_org_memories_tsv ON org_memories`,
	`CREATE TRIGGER trg_org_memories_tsv
		BEFORE INSERT OR UPDATE OF chunk_text ON org_memories
		FOR EACH ROW EXECUTE FUNCTION org_memories_tsv_trigger()`,

	// --- Row-Level Security on team_memberships ---
	`ALTER TABLE team_memberships ENABLE ROW LEVEL SECURITY`,

	`DO $$ BEGIN
		IF NOT EXISTS (
			SELECT 1 FROM pg_policies WHERE policyname = 'tm_isolation' AND tablename = 'team_memberships'
		) THEN
			CREATE POLICY tm_isolation ON team_memberships
			USING (
				user_id = current_setting('app.current_user', true)::UUID
				OR team_id IN (
					SELECT team_id FROM team_memberships
					WHERE user_id = current_setting('app.current_user', true)::UUID
					AND role = 'admin'
				)
			);
		END IF;
	END $$`,
}

// =============================================================================
// Team extras
// =============================================================================

var teamExtras = []string{
	// --- Specialized indexes ---
	// Note: In entstore architecture, each team has its own database with tables
	// in the public schema. No {{schema}} placeholder needed.
	`CREATE INDEX IF NOT EXISTS idx_memories_embedding
		ON memories USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`,

	`CREATE INDEX IF NOT EXISTS idx_team_memories_tsv
		ON memories USING GIN (tsv)`,

	`CREATE INDEX IF NOT EXISTS idx_memories_session_id
		ON memories (session_id) WHERE session_id IS NOT NULL`,

	// --- Partial indexes ---
	`CREATE INDEX IF NOT EXISTS idx_sessions_parent
		ON sessions(parent_id) WHERE parent_id IS NOT NULL`,

	`CREATE INDEX IF NOT EXISTS idx_sessions_fleet
		ON sessions(fleet_key) WHERE fleet_key != ''`,

	`CREATE INDEX IF NOT EXISTS idx_flows_type
		ON flows(type) WHERE type != ''`,

	`CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_next_run
		ON scheduled_jobs(next_run_at) WHERE status = 'active'`,

	// Sandbox session partial indexes
	`CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_state_active
		ON sandbox_sessions(state, last_active_at)
		WHERE state IN ('running', 'evicted')`,

	`CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_upper_layer
		ON sandbox_sessions(upper_layer_id)
		WHERE upper_layer_id IS NOT NULL`,

	`CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_container
		ON sandbox_sessions(container_name)
		WHERE container_name IS NOT NULL`,

	// --- tsvector trigger ---
	`CREATE OR REPLACE FUNCTION memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('english', NEW.chunk_text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql`,

	`DROP TRIGGER IF EXISTS trg_memories_tsv ON memories`,
	`CREATE TRIGGER trg_memories_tsv
		BEFORE INSERT OR UPDATE OF chunk_text ON memories
		FOR EACH ROW EXECUTE FUNCTION memories_tsv_trigger()`,
}

// =============================================================================
// Personal extras
// =============================================================================

var personalExtras = []string{
	// --- Specialized indexes ---
	`CREATE INDEX IF NOT EXISTS idx_personal_memories_embedding
		ON memories USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)`,

	`CREATE INDEX IF NOT EXISTS idx_personal_memories_tsv
		ON memories USING GIN (tsv)`,

	`CREATE INDEX IF NOT EXISTS idx_memories_session_id
		ON memories (session_id) WHERE session_id IS NOT NULL`,

	// --- tsvector trigger ---
	`CREATE OR REPLACE FUNCTION memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('english', NEW.chunk_text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql`,

	`DROP TRIGGER IF EXISTS trg_memories_tsv ON memories`,
	`CREATE TRIGGER trg_memories_tsv
		BEFORE INSERT OR UPDATE OF chunk_text ON memories
		FOR EACH ROW EXECUTE FUNCTION memories_tsv_trigger()`,
}
