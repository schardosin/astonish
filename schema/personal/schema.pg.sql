-- Personal schema (PostgreSQL) — desired state.
-- Source of truth for Atlas diff generation.
-- Applied to: astonish_org_{slug} database, personal_{user_id} schema.
-- Note: {{schema}} is replaced with the actual schema name at migration time.

-- ============================================================================
-- Memories (vector + BM25 hybrid search)
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.memories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chunk_text      TEXT NOT NULL,
    embedding       vector(384),
    tsv             tsvector,
    category        TEXT,
    source_path     TEXT,
    metadata        JSONB,
    created_by      UUID,
    session_id      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_personal_memories_embedding
    ON {{schema}}.memories USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

CREATE INDEX IF NOT EXISTS idx_personal_memories_tsv
    ON {{schema}}.memories USING GIN (tsv);

CREATE INDEX IF NOT EXISTS idx_memories_session_id
    ON {{schema}}.memories (session_id) WHERE session_id IS NOT NULL;

-- Auto-update tsvector on INSERT/UPDATE
CREATE OR REPLACE FUNCTION {{schema}}.memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('english', NEW.chunk_text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_memories_tsv ON {{schema}}.memories;
CREATE TRIGGER trg_memories_tsv
    BEFORE INSERT OR UPDATE OF chunk_text ON {{schema}}.memories
    FOR EACH ROW EXECUTE FUNCTION {{schema}}.memories_tsv_trigger();

-- ============================================================================
-- Apps and app state
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.apps (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug            TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    description     TEXT DEFAULT '',
    code            TEXT NOT NULL DEFAULT '',
    version         INTEGER DEFAULT 1,
    session_id      TEXT DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.app_state (
    app_id          UUID NOT NULL,
    key             TEXT NOT NULL,
    value           JSONB,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (app_id, key)
);

-- ============================================================================
-- Sessions and events
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.sessions (
    id              TEXT PRIMARY KEY,
    user_id         UUID,
    title           TEXT DEFAULT '',
    message_count   INTEGER DEFAULT 0,
    parent_id       TEXT,
    fleet_key       TEXT NOT NULL DEFAULT '',
    fleet_name      TEXT NOT NULL DEFAULT '',
    issue_number    INTEGER NOT NULL DEFAULT 0,
    repo            TEXT NOT NULL DEFAULT '',
    workspace_dir   TEXT NOT NULL DEFAULT '',
    metadata        JSONB DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.session_events (
    id              BIGSERIAL PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES {{schema}}.sessions(id) ON DELETE CASCADE,
    event_data      JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Flows and credentials
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.flows (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    definition      JSONB NOT NULL DEFAULT '{}',
    yaml_content    TEXT,
    type            TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    cred_type       TEXT NOT NULL,
    encrypted       BYTEA NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Indexes
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_personal_session_events_session
    ON {{schema}}.session_events(session_id);
CREATE INDEX IF NOT EXISTS idx_personal_sessions_updated
    ON {{schema}}.sessions(updated_at);
CREATE INDEX IF NOT EXISTS idx_personal_flows_type
    ON {{schema}}.flows(type);
