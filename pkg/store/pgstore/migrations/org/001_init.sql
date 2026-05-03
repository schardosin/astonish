-- Org database initialization (public schema)
-- Applied to: astonish_org_{slug} database, public schema
-- Contains: org-wide shared tables — teams, shared memories, skills, apps, audit

-- Enable pgvector for embedding storage
CREATE EXTENSION IF NOT EXISTS vector;

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

-- RLS on team_memberships: users see only their own memberships
-- (or all memberships for teams they admin).
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

CREATE TABLE IF NOT EXISTS org_memories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chunk_text      TEXT NOT NULL,
    embedding       vector(384),
    category        TEXT,
    source_path     TEXT,
    metadata        JSONB,
    promoted_by     UUID NOT NULL,
    promoted_from_team TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_org_memories_embedding
    ON org_memories USING ivfflat (embedding vector_cosine_ops)
    WITH (lists = 100);

CREATE TABLE IF NOT EXISTS org_skills (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    content     TEXT NOT NULL,
    frontmatter JSONB,
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

-- Audit log is append-only: the app role gets INSERT only (no UPDATE/DELETE).
-- This is enforced at the GRANT level in provision.go.

-- Indexes
CREATE INDEX IF NOT EXISTS idx_team_memberships_team ON team_memberships(team_id);
CREATE INDEX IF NOT EXISTS idx_team_memberships_user ON team_memberships(user_id);
CREATE INDEX IF NOT EXISTS idx_org_audit_log_user ON org_audit_log(user_id);
CREATE INDEX IF NOT EXISTS idx_org_audit_log_timestamp ON org_audit_log(timestamp);
CREATE INDEX IF NOT EXISTS idx_org_audit_log_action ON org_audit_log(action);
