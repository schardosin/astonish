-- Team schema (PostgreSQL) — desired state.
-- Source of truth for Atlas diff generation.
-- Applied to: astonish_org_{slug} database, team_{team_slug} schema.
-- Note: {{schema}} is replaced with the actual schema name at migration time.

-- ============================================================================
-- Sessions and events
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.sessions (
    id              TEXT PRIMARY KEY,
    user_id         UUID,
    title           TEXT DEFAULT '',
    message_count   INTEGER DEFAULT 0,
    parent_id       TEXT,
    fleet_key       TEXT DEFAULT '',
    fleet_name      TEXT DEFAULT '',
    issue_number    INTEGER NOT NULL DEFAULT 0,
    repo            TEXT NOT NULL DEFAULT '',
    workspace_dir   TEXT DEFAULT '',
    metadata        JSONB DEFAULT '{}',
    last_seq        BIGINT NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.session_events (
    id              BIGSERIAL PRIMARY KEY,
    session_id      TEXT NOT NULL REFERENCES {{schema}}.sessions(id) ON DELETE CASCADE,
    event_data      JSONB NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_session_events_session
    ON {{schema}}.session_events(session_id);

-- ============================================================================
-- Memories (vector + BM25 hybrid search)
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.memories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by      UUID,
    chunk_text      TEXT NOT NULL,
    embedding       vector(384),
    tsv             tsvector,
    category        TEXT,
    source_path     TEXT,
    metadata        JSONB,
    session_id      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memories_embedding
    ON {{schema}}.memories USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

CREATE INDEX IF NOT EXISTS idx_team_memories_tsv
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
-- Credentials (encrypted at rest)
-- ============================================================================

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
    published_by    UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.app_state (
    app_id          UUID NOT NULL,
    user_id         UUID NOT NULL,
    key             TEXT NOT NULL,
    value           JSONB,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (app_id, user_id, key)
);

-- ============================================================================
-- Flows (agent definitions / YAML workflows)
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.flows (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    definition      JSONB NOT NULL,
    yaml_content    TEXT,
    type            TEXT NOT NULL DEFAULT '',
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Scheduled jobs
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.scheduled_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    schedule        TEXT NOT NULL,
    mode            TEXT NOT NULL DEFAULT 'routine'
                    CHECK (mode IN ('routine', 'adaptive', 'fleet_poll')),
    payload         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'paused', 'completed', 'failed')),
    last_run_at     TIMESTAMPTZ,
    next_run_at     TIMESTAMPTZ,
    last_status     TEXT NOT NULL DEFAULT 'pending',
    last_error      TEXT NOT NULL DEFAULT '',
    consecutive_failures INT NOT NULL DEFAULT 0,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Fleet templates and plans
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.fleet_templates (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key             TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    definition      JSONB NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.fleet_plans (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key             TEXT NOT NULL UNIQUE,
    name            TEXT NOT NULL,
    definition      JSONB NOT NULL,
    yaml_content    TEXT,
    active          BOOLEAN DEFAULT false,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.fleet_monitor_state (
    plan_key    TEXT PRIMARY KEY,
    state       JSONB NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Audit log, drill reports, skills, skill files, MCP servers
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.team_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_id         UUID NOT NULL,
    action          TEXT NOT NULL,
    resource        TEXT NOT NULL,
    detail          JSONB,
    session_id      TEXT
);

CREATE TABLE IF NOT EXISTS {{schema}}.drill_reports (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    suite           TEXT NOT NULL,
    status          TEXT NOT NULL,
    summary         TEXT DEFAULT '',
    duration_ms     BIGINT DEFAULT 0,
    report_data     JSONB NOT NULL,
    started_at      TIMESTAMPTZ NOT NULL,
    finished_at     TIMESTAMPTZ NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.skills (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    content         TEXT NOT NULL,
    frontmatter     JSONB,
    validation_status TEXT NOT NULL DEFAULT 'unknown',
    validation_meta   JSONB,
    created_by      UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.skill_files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id        UUID NOT NULL REFERENCES {{schema}}.skills(id) ON DELETE CASCADE,
    path            TEXT NOT NULL DEFAULT '',
    filename        TEXT NOT NULL,
    content         TEXT NOT NULL,
    is_executable   BOOLEAN NOT NULL DEFAULT false,
    size_bytes      BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(skill_id, path, filename)
);

CREATE INDEX IF NOT EXISTS skill_files_skill_id_idx ON {{schema}}.skill_files(skill_id);
CREATE INDEX IF NOT EXISTS skill_files_path_idx ON {{schema}}.skill_files(path);

CREATE TABLE IF NOT EXISTS {{schema}}.mcp_servers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    command         TEXT,
    args            JSONB DEFAULT '[]'::jsonb,
    env             JSONB DEFAULT '{}'::jsonb,
    transport       TEXT NOT NULL DEFAULT 'stdio',
    url             TEXT,
    enabled         BOOLEAN NOT NULL DEFAULT true,
    cached_tools    JSONB,
    created_by      UUID NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Settings (team-level key-value)
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.settings (
    key         TEXT PRIMARY KEY,
    value       JSONB NOT NULL DEFAULT '{}',
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Sandbox sessions and chat session events
-- ============================================================================

CREATE TABLE IF NOT EXISTS {{schema}}.sandbox_sessions (
    id                 TEXT PRIMARY KEY,
    chat_session_id    TEXT NOT NULL,
    backend            TEXT NOT NULL DEFAULT 'incus',
    container_name     TEXT,
    template_id        UUID NOT NULL,
    upper_layer_id     TEXT,
    state              TEXT NOT NULL DEFAULT 'creating'
                       CHECK (state IN ('creating', 'running', 'evicting', 'evicted', 'resuming', 'terminated')),
    pod_name           TEXT,
    node_name          TEXT,
    exposed_ports      JSONB NOT NULL DEFAULT '[]'::jsonb,
    base_domain        TEXT,
    pinned             BOOLEAN NOT NULL DEFAULT FALSE,
    created_by         UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.chat_session_events (
    chat_session_id  TEXT NOT NULL REFERENCES {{schema}}.sessions(id) ON DELETE CASCADE,
    seq              BIGINT NOT NULL,
    event_type       TEXT NOT NULL,
    payload          JSONB NOT NULL,
    producer_pod     TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_session_id, seq)
);

-- ============================================================================
-- Indexes
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_sessions_parent
    ON {{schema}}.sessions(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_fleet
    ON {{schema}}.sessions(fleet_key) WHERE fleet_key != '';
CREATE INDEX IF NOT EXISTS idx_sessions_updated
    ON {{schema}}.sessions(updated_at);
CREATE INDEX IF NOT EXISTS idx_flows_type
    ON {{schema}}.flows(type) WHERE type != '';
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_status
    ON {{schema}}.scheduled_jobs(status);
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_next_run
    ON {{schema}}.scheduled_jobs(next_run_at) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_team_audit_log_timestamp
    ON {{schema}}.team_audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_drill_reports_suite
    ON {{schema}}.drill_reports(suite);
CREATE INDEX IF NOT EXISTS idx_drill_reports_created
    ON {{schema}}.drill_reports(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_skills_name
    ON {{schema}}.skills(name);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_name
    ON {{schema}}.mcp_servers(name);
CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_chat
    ON {{schema}}.sandbox_sessions(chat_session_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_state_active
    ON {{schema}}.sandbox_sessions(state, last_active_at)
    WHERE state IN ('running', 'evicted');
CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_upper_layer
    ON {{schema}}.sandbox_sessions(upper_layer_id)
    WHERE upper_layer_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_container
    ON {{schema}}.sandbox_sessions(container_name)
    WHERE container_name IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_chat_session_events_created
    ON {{schema}}.chat_session_events(chat_session_id, created_at);
