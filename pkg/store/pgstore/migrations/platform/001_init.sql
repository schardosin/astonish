-- Platform database initialization (v1.0)
-- Applied to: astonish_platform database, public schema
-- Contains: cross-org authentication, organization registry, channel links, settings

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
    password_hash   TEXT,                -- bcrypt, NULL for OIDC-only users
    oidc_subject    TEXT,
    oidc_issuer     TEXT,
    platform_role   TEXT DEFAULT NULL    -- NULL = regular user, 'superadmin' = platform administrator
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
    org_id          UUID REFERENCES organizations(id) ON DELETE CASCADE,  -- NULL = platform-wide
    name            TEXT NOT NULL DEFAULT '',    -- Human-readable display name (e.g. "SAP IAS", "Azure AD")
    issuer_url      TEXT NOT NULL,
    discovery_url   TEXT NOT NULL DEFAULT '',    -- Base URL for .well-known discovery (if different from issuer)
    client_id       TEXT NOT NULL,
    client_secret   TEXT NOT NULL,    -- encrypted at rest via application-level encryption
    scopes          TEXT[] DEFAULT ARRAY['openid', 'email', 'profile'],
    team_claim      TEXT,             -- OIDC claim name for automatic team mapping
    enabled         BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(org_id, issuer_url)
);

-- ============================================================================
-- Login sessions (JWT refresh tokens)
-- ============================================================================

CREATE TABLE IF NOT EXISTS login_sessions (
    token_hash  TEXT PRIMARY KEY,     -- SHA-256 of refresh token
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
    channel_type    VARCHAR(32) NOT NULL,        -- 'telegram', 'email', 'slack'
    external_id     VARCHAR(255) NOT NULL,       -- TG user ID, email address, Slack user ID
    display_name    VARCHAR(255) DEFAULT '',     -- @username or email label
    enabled         BOOLEAN DEFAULT true,
    verified        BOOLEAN DEFAULT false,       -- true after verification handshake
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
