-- Team schema initialization
-- Applied to: astonish_org_{slug} database, team_{team_slug} schema
-- Contains: all team-scoped data tables
-- Note: {{schema}} is replaced with the actual schema name at migration time

CREATE TABLE IF NOT EXISTS {{schema}}.sessions (
    id              TEXT PRIMARY KEY,
    user_id         UUID,
    title           TEXT DEFAULT '',
    message_count   INTEGER DEFAULT 0,
    parent_id       TEXT,
    fleet_key       TEXT DEFAULT '',
    fleet_name      TEXT DEFAULT '',
    workspace_dir   TEXT DEFAULT '',
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

CREATE INDEX IF NOT EXISTS idx_session_events_session
    ON {{schema}}.session_events(session_id);

CREATE TABLE IF NOT EXISTS {{schema}}.memories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by      UUID,
    chunk_text      TEXT NOT NULL,
    embedding       vector(384),
    category        TEXT,
    source_path     TEXT,
    metadata        JSONB,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_memories_embedding
    ON {{schema}}.memories USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

CREATE TABLE IF NOT EXISTS {{schema}}.credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    cred_type       TEXT NOT NULL,
    encrypted       BYTEA NOT NULL,     -- AES-256-GCM encrypted credential data
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

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

CREATE TABLE IF NOT EXISTS {{schema}}.flows (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL UNIQUE,
    definition      JSONB NOT NULL,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.scheduled_jobs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            TEXT NOT NULL,
    schedule        TEXT NOT NULL,          -- cron expression
    mode            TEXT NOT NULL DEFAULT 'routine'
                    CHECK (mode IN ('routine', 'adaptive', 'fleet_poll')),
    payload         JSONB NOT NULL DEFAULT '{}',
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'paused', 'completed', 'failed')),
    last_run_at     TIMESTAMPTZ,
    next_run_at     TIMESTAMPTZ,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

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
    active          BOOLEAN DEFAULT false,
    created_by      UUID,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS {{schema}}.team_audit_log (
    id              BIGSERIAL PRIMARY KEY,
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT now(),
    user_id         UUID NOT NULL,
    action          TEXT NOT NULL,
    resource        TEXT NOT NULL,
    detail          JSONB,
    session_id      TEXT
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_sessions_parent
    ON {{schema}}.sessions(parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_fleet
    ON {{schema}}.sessions(fleet_key) WHERE fleet_key != '';
CREATE INDEX IF NOT EXISTS idx_sessions_updated
    ON {{schema}}.sessions(updated_at);
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_status
    ON {{schema}}.scheduled_jobs(status);
CREATE INDEX IF NOT EXISTS idx_scheduled_jobs_next_run
    ON {{schema}}.scheduled_jobs(next_run_at) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_team_audit_log_timestamp
    ON {{schema}}.team_audit_log(timestamp);
