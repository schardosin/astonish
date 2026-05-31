-- Platform schema (PostgreSQL) — desired state.
-- Source of truth for Atlas diff generation.
-- Applied to: astonish_platform database, public schema.

-- Enable pgvector for embedding storage (used by tool_index)
CREATE EXTENSION IF NOT EXISTS vector;

-- ============================================================================
-- Core tables: organizations, users, memberships
-- ============================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL,
    slug        TEXT NOT NULL UNIQUE,
    db_name     TEXT NOT NULL UNIQUE,
    status      TEXT NOT NULL DEFAULT 'active'
                CHECK (status IN ('active', 'suspended', 'decommissioned')),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    settings    JSONB DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL UNIQUE,
    display_name    TEXT NOT NULL,
    password_hash   TEXT,
    oidc_subject    TEXT,
    oidc_issuer     TEXT,
    platform_role   TEXT DEFAULT NULL
                    CHECK (platform_role IS NULL OR platform_role IN ('superadmin')),
    status          TEXT NOT NULL DEFAULT 'active'
                    CHECK (status IN ('active', 'suspended', 'deactivated')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_login_at   TIMESTAMPTZ,
    UNIQUE(oidc_issuer, oidc_subject)
);

CREATE TABLE IF NOT EXISTS org_memberships (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role        TEXT NOT NULL DEFAULT 'member'
                CHECK (role IN ('owner', 'admin', 'member')),
    joined_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, org_id)
);

-- ============================================================================
-- OIDC providers (SSO configuration)
-- ============================================================================

CREATE TABLE IF NOT EXISTS oidc_providers (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          UUID REFERENCES organizations(id) ON DELETE CASCADE,
    name            TEXT NOT NULL DEFAULT '',
    issuer_url      TEXT NOT NULL,
    discovery_url   TEXT NOT NULL DEFAULT '',
    client_id       TEXT NOT NULL,
    client_secret   TEXT NOT NULL,
    scopes          TEXT[] DEFAULT ARRAY['openid', 'email', 'profile'],
    team_claim      TEXT,
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, issuer_url)
);

-- ============================================================================
-- Login sessions (JWT refresh tokens)
-- ============================================================================

CREATE TABLE IF NOT EXISTS login_sessions (
    token_hash  TEXT PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id      UUID NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    user_agent  TEXT,
    ip_address  INET
);

-- ============================================================================
-- User channel links (external messaging: Telegram, Email, Slack)
-- ============================================================================

CREATE TABLE IF NOT EXISTS user_channels (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type    VARCHAR(32) NOT NULL,
    external_id     VARCHAR(255) NOT NULL,
    display_name    VARCHAR(255) DEFAULT '',
    enabled         BOOLEAN DEFAULT true,
    verified        BOOLEAN DEFAULT false,
    verified_at     TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(channel_type, external_id)
);

-- ============================================================================
-- Platform secrets (instance-wide encrypted key-value store)
-- ============================================================================

CREATE TABLE IF NOT EXISTS platform_secrets (
    key         TEXT PRIMARY KEY,
    value       BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================================
-- Email thread index (maps Message-ID to session for thread routing)
-- ============================================================================

CREATE TABLE IF NOT EXISTS email_thread_index (
    message_id  TEXT PRIMARY KEY,
    session_key TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Platform settings (provider configuration, defaults)
-- ============================================================================

CREATE TABLE IF NOT EXISTS platform_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Tool index (semantic tool discovery via pgvector)
-- ============================================================================

CREATE TABLE IF NOT EXISTS tool_index (
    id         TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    embedding  vector(384),
    metadata   JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Platform MCP servers (inherited by all orgs/teams)
-- ============================================================================

CREATE TABLE IF NOT EXISTS platform_mcp_servers (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    name        TEXT UNIQUE NOT NULL,
    command     TEXT,
    args        JSONB DEFAULT '[]',
    env         JSONB DEFAULT '{}',
    transport   TEXT NOT NULL DEFAULT 'stdio',
    url         TEXT,
    enabled     BOOLEAN NOT NULL DEFAULT true,
    cached_tools JSONB,
    created_by  TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Device sessions (transient SSO/OIDC device flow state)
-- ============================================================================

CREATE TABLE IF NOT EXISTS device_sessions (
    device_code   TEXT PRIMARY KEY,
    state         TEXT UNIQUE NOT NULL,
    nonce         TEXT NOT NULL,
    provider_id   TEXT NOT NULL,
    client_type   TEXT NOT NULL DEFAULT 'cli',
    status        TEXT NOT NULL DEFAULT 'pending',
    error_message TEXT,
    result_data   JSONB,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at    TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '10 minutes')
);

-- ============================================================================
-- Pending link codes (transient channel-linking verification)
-- ============================================================================

CREATE TABLE IF NOT EXISTS pending_link_codes (
    code       CHAR(6) PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      TEXT,
    channel    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '5 minutes')
);

-- ============================================================================
-- Sandbox layers (content-addressed layer registry)
-- ============================================================================

CREATE TABLE IF NOT EXISTS sandbox_layers (
    layer_id        TEXT PRIMARY KEY,
    parent_layer    TEXT REFERENCES sandbox_layers(layer_id),
    cephfs_path     TEXT NOT NULL,
    size_bytes      BIGINT NOT NULL DEFAULT 0,
    ref_count       INTEGER NOT NULL DEFAULT 0
                    CHECK (ref_count >= 0),
    created_by      UUID,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_referenced TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- ============================================================================
-- Sandbox templates (template DAG)
-- ============================================================================

CREATE TABLE IF NOT EXISTS sandbox_templates (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                 TEXT NOT NULL,
    scope                TEXT NOT NULL
                         CHECK (scope IN ('global', 'org', 'team', 'personal')),
    owner_id             TEXT NOT NULL DEFAULT '',
    purpose              TEXT NOT NULL DEFAULT ''
                         CHECK (purpose IN ('', 'fleet')),
    name                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    parent_template_id   UUID REFERENCES sandbox_templates(id) ON DELETE RESTRICT,
    top_layer_id         TEXT REFERENCES sandbox_layers(layer_id) ON DELETE RESTRICT,
    base_config          JSONB,
    configured_by        UUID,
    configured_at        TIMESTAMPTZ,
    version              INTEGER NOT NULL DEFAULT 1,
    created_by           UUID,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE (scope, owner_id, slug),

    CONSTRAINT sandbox_templates_root_is_base
        CHECK (
            parent_template_id IS NOT NULL
            OR (scope = 'global' AND slug = 'base')
        )
);

-- ============================================================================
-- Indexes
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_org_memberships_org ON org_memberships(org_id);
CREATE INDEX IF NOT EXISTS idx_org_memberships_user ON org_memberships(user_id);
CREATE INDEX IF NOT EXISTS idx_login_sessions_user ON login_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_login_sessions_expires ON login_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_users_oidc ON users(oidc_issuer, oidc_subject) WHERE oidc_issuer IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_user_channels_user ON user_channels(user_id);
CREATE INDEX IF NOT EXISTS idx_user_channels_lookup ON user_channels(channel_type, external_id);
CREATE INDEX IF NOT EXISTS idx_user_channels_type_enabled ON user_channels(channel_type, enabled, verified);
CREATE INDEX IF NOT EXISTS idx_email_thread_index_session ON email_thread_index(session_key);
CREATE INDEX IF NOT EXISTS idx_tool_index_embedding ON tool_index USING hnsw (embedding vector_cosine_ops);
CREATE INDEX IF NOT EXISTS idx_device_sessions_state ON device_sessions(state);
CREATE INDEX IF NOT EXISTS idx_device_sessions_expires ON device_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_pending_link_codes_user_channel ON pending_link_codes(user_id, channel);
CREATE INDEX IF NOT EXISTS idx_pending_link_codes_expires ON pending_link_codes(expires_at);
CREATE INDEX IF NOT EXISTS idx_sandbox_layers_unreferenced ON sandbox_layers(added_at) WHERE ref_count = 0;
CREATE INDEX IF NOT EXISTS idx_sandbox_layers_parent ON sandbox_layers(parent_layer) WHERE parent_layer IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sandbox_templates_scope_owner ON sandbox_templates(scope, owner_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_templates_parent ON sandbox_templates(parent_template_id) WHERE parent_template_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sandbox_templates_top_layer ON sandbox_templates(top_layer_id) WHERE top_layer_id IS NOT NULL;

-- ============================================================================
-- Cycle-detection trigger
-- ============================================================================

CREATE OR REPLACE FUNCTION sandbox_templates_check_no_cycle() RETURNS trigger AS $$
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
            RAISE EXCEPTION
                'cycle detected in sandbox_templates parent chain at template %',
                NEW.id;
        END IF;
        depth := depth + 1;
        IF depth > 100 THEN
            RAISE EXCEPTION
                'sandbox_templates parent chain exceeds depth 100 (possible cycle)';
        END IF;
        SELECT parent_template_id INTO current_id
          FROM sandbox_templates
         WHERE id = current_id;
    END LOOP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sandbox_templates_no_cycle ON sandbox_templates;
CREATE TRIGGER trg_sandbox_templates_no_cycle
    BEFORE INSERT OR UPDATE OF parent_template_id ON sandbox_templates
    FOR EACH ROW EXECUTE FUNCTION sandbox_templates_check_no_cycle();

-- ============================================================================
-- Ref-count backstop (DISABLED by default — diagnostic only)
-- ============================================================================

CREATE OR REPLACE FUNCTION sandbox_layers_bump_ref(layer TEXT, delta INTEGER) RETURNS void AS $$
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
$$ LANGUAGE plpgsql;

CREATE OR REPLACE FUNCTION sandbox_templates_ref_count_backstop() RETURNS trigger AS $$
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
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_sandbox_templates_ref_backstop ON sandbox_templates;
CREATE TRIGGER trg_sandbox_templates_ref_backstop
    AFTER INSERT OR UPDATE OF top_layer_id OR DELETE ON sandbox_templates
    FOR EACH ROW EXECUTE FUNCTION sandbox_templates_ref_count_backstop();

ALTER TABLE sandbox_templates
    DISABLE TRIGGER trg_sandbox_templates_ref_backstop;
