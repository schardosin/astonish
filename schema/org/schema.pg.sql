-- Org schema (PostgreSQL) — desired state.
-- Source of truth for Atlas diff generation.
-- Applied to: astonish_org_{slug} database, public schema.

-- Enable pgvector for embedding storage
CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================================================
-- Teams and memberships
-- ============================================================================

CREATE TABLE IF NOT EXISTS teams (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    schema_name TEXT NOT NULL UNIQUE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    settings    JSONB DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS team_memberships (
    user_id     UUID NOT NULL,
    team_id     UUID NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member'
                CHECK (role IN ('admin', 'member', 'viewer')),
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, team_id)
);

-- RLS on team_memberships
ALTER TABLE team_memberships ENABLE ROW LEVEL SECURITY;

CREATE POLICY tm_isolation ON team_memberships
    USING (
        user_id = current_setting('app.current_user', true)::UUID
        OR team_id IN (
            SELECT team_id FROM team_memberships
            WHERE user_id = current_setting('app.current_user', true)::UUID
            AND role = 'admin'
        )
    );

-- ============================================================================
-- Org-wide memories (promoted from team/personal)
-- ============================================================================

CREATE TABLE IF NOT EXISTS org_memories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chunk_text      TEXT NOT NULL,
    embedding       vector(384),
    tsv             tsvector,
    category        TEXT,
    source_path     TEXT,
    metadata        JSONB,
    promoted_by     UUID NOT NULL,
    promoted_from_team TEXT,
    session_id      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_org_memories_embedding
    ON org_memories USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

CREATE INDEX IF NOT EXISTS idx_org_memories_tsv
    ON org_memories USING GIN (tsv);

CREATE INDEX IF NOT EXISTS idx_org_memories_session_id
    ON org_memories (session_id) WHERE session_id IS NOT NULL;

-- Auto-update tsvector on INSERT/UPDATE
CREATE OR REPLACE FUNCTION org_memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('english', NEW.chunk_text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_org_memories_tsv ON org_memories;
CREATE TRIGGER trg_org_memories_tsv
    BEFORE INSERT OR UPDATE OF chunk_text ON org_memories
    FOR EACH ROW EXECUTE FUNCTION org_memories_tsv_trigger();

-- ============================================================================
-- Org-wide skills, skill files, MCP servers, apps
-- ============================================================================

CREATE TABLE IF NOT EXISTS org_skills (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    content     TEXT NOT NULL,
    frontmatter JSONB,
    validation_status TEXT NOT NULL DEFAULT 'unknown',
    validation_meta   JSONB,
    created_by  UUID NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS org_skill_files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id        UUID NOT NULL REFERENCES org_skills(id) ON DELETE CASCADE,
    path            TEXT NOT NULL DEFAULT '',
    filename        TEXT NOT NULL,
    content         TEXT NOT NULL,
    is_executable   BOOLEAN NOT NULL DEFAULT false,
    size_bytes      BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(skill_id, path, filename)
);

CREATE INDEX IF NOT EXISTS org_skill_files_skill_id_idx ON org_skill_files(skill_id);
CREATE INDEX IF NOT EXISTS org_skill_files_path_idx ON org_skill_files(path);

CREATE TABLE IF NOT EXISTS org_mcp_servers (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    command     TEXT,
    args        JSONB DEFAULT '[]'::jsonb,
    env         JSONB DEFAULT '{}'::jsonb,
    transport   TEXT NOT NULL DEFAULT 'stdio',
    url         TEXT,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    cached_tools JSONB,
    created_by  UUID NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS org_apps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    description     TEXT DEFAULT '',
    definition      JSONB NOT NULL,
    promoted_by     UUID NOT NULL,
    promoted_from_team TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Audit log (append-only)
-- ============================================================================

CREATE TABLE IF NOT EXISTS org_audit_log (
    id          BIGSERIAL PRIMARY KEY,
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_id     UUID NOT NULL,
    team_id     TEXT,
    action      TEXT NOT NULL,
    resource    TEXT NOT NULL,
    detail      JSONB,
    ip_address  INET,
    session_id  TEXT
);

-- ============================================================================
-- Encryption keys (envelope encryption for credentials)
-- ============================================================================

CREATE TABLE IF NOT EXISTS org_encryption_keys (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key_name    TEXT NOT NULL UNIQUE,
    key_data    BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Indexes
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_team_memberships_team ON team_memberships(team_id);
CREATE INDEX IF NOT EXISTS idx_team_memberships_user ON team_memberships(user_id);
CREATE INDEX IF NOT EXISTS idx_org_audit_log_user ON org_audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_org_audit_log_timestamp ON org_audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_org_audit_log_action ON org_audit_log(action);
CREATE INDEX IF NOT EXISTS idx_org_mcp_servers_name ON org_mcp_servers(name);
