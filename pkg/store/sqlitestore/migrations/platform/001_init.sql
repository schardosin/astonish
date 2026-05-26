-- Platform database schema.
-- Contains: users, organizations, login sessions, OIDC providers,
-- user channels, platform secrets, settings, tool index, MCP servers,
-- sandbox layers, sandbox templates, email threads, link codes, device sessions.

-- ==========================================================================
-- Core identity and org tables
-- ==========================================================================

CREATE TABLE IF NOT EXISTS organizations (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','suspended','decommissioned')),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    settings TEXT DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS users (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    password_hash TEXT,
    oidc_subject TEXT,
    oidc_issuer TEXT,
    platform_role TEXT NOT NULL DEFAULT 'member' CHECK (platform_role IN ('superadmin','admin','member')),
    status TEXT NOT NULL DEFAULT 'active',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_login_at TEXT,
    UNIQUE(oidc_issuer, oidc_subject)
);

CREATE TABLE IF NOT EXISTS org_memberships (
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
    joined_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, org_id)
);

CREATE TABLE IF NOT EXISTS oidc_providers (
    id TEXT PRIMARY KEY,
    org_id TEXT REFERENCES organizations(id),
    name TEXT NOT NULL,
    issuer_url TEXT NOT NULL,
    discovery_url TEXT,
    client_id TEXT NOT NULL,
    client_secret TEXT NOT NULL,
    scopes TEXT DEFAULT '["openid","email","profile"]',
    team_claim TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(org_id, issuer_url)
);

CREATE TABLE IF NOT EXISTS login_sessions (
    token_hash TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    org_id TEXT NOT NULL REFERENCES organizations(id),
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL,
    user_agent TEXT,
    ip_address TEXT
);

CREATE TABLE IF NOT EXISTS user_channels (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type TEXT NOT NULL,
    external_id TEXT NOT NULL,
    display_name TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    verified INTEGER NOT NULL DEFAULT 0,
    verified_at TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(channel_type, external_id)
);

-- ==========================================================================
-- Platform secrets, settings, tool index, MCP servers
-- ==========================================================================

CREATE TABLE IF NOT EXISTS platform_secrets (
    key TEXT PRIMARY KEY,
    value BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS platform_settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS tool_index (
    id TEXT PRIMARY KEY,
    content TEXT NOT NULL,
    embedding BLOB,
    metadata TEXT,
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS platform_mcp_servers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    command TEXT,
    args TEXT,
    env TEXT,
    transport TEXT,
    url TEXT,
    enabled INTEGER NOT NULL DEFAULT 1,
    cached_tools TEXT,
    created_by TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

-- ==========================================================================
-- Email threads, link codes, device sessions
-- ==========================================================================

CREATE TABLE IF NOT EXISTS email_thread_index (
    message_id TEXT PRIMARY KEY,
    session_key TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_email_thread_session ON email_thread_index(session_key);

CREATE TABLE IF NOT EXISTS pending_link_codes (
    code TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    email TEXT NOT NULL,
    channel TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_pending_link_codes_expires ON pending_link_codes(expires_at);

CREATE TABLE IF NOT EXISTS device_sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL,
    device_name TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    expires_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_device_sessions_expires ON device_sessions(expires_at);

-- ==========================================================================
-- Sandbox layers and templates
-- ==========================================================================

CREATE TABLE IF NOT EXISTS sandbox_layers (
    layer_id TEXT PRIMARY KEY,
    parent_layer TEXT,
    cephfs_path TEXT NOT NULL,
    size_bytes INTEGER NOT NULL DEFAULT 0,
    ref_count INTEGER NOT NULL DEFAULT 0,
    created_by TEXT,
    added_at TEXT NOT NULL DEFAULT (datetime('now')),
    last_referenced TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS sandbox_templates (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT 'global',
    owner_id TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    parent_template_id TEXT,
    top_layer_id TEXT,
    base_config BLOB,
    configured_by TEXT,
    configured_at TEXT,
    version INTEGER NOT NULL DEFAULT 1,
    created_by TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(scope, owner_id, slug),
    FOREIGN KEY (parent_template_id) REFERENCES sandbox_templates(id)
);

CREATE INDEX IF NOT EXISTS idx_sandbox_templates_scope ON sandbox_templates(scope, owner_id);
CREATE INDEX IF NOT EXISTS idx_sandbox_templates_parent ON sandbox_templates(parent_template_id);

-- ==========================================================================
-- Seed global @base layer and template (DAG root)
-- ==========================================================================

-- Layer row first (FK target for the template).
INSERT OR IGNORE INTO sandbox_layers (layer_id, parent_layer, cephfs_path, size_bytes, ref_count)
VALUES ('@base', NULL, '/mnt/astonish-layers/@base', 0, 1);

-- Template row: global scope, slug='base', no parent (the DAG root).
-- Uses the same well-known UUID as PG (UUID v5 of "astonish:base-template").
INSERT OR IGNORE INTO sandbox_templates (id, slug, scope, owner_id, purpose, name, description, parent_template_id, top_layer_id, version)
VALUES (
    'a0000000-0000-4000-8000-000000000001',
    'base',
    'global',
    '',
    '',
    'Base',
    'Deployment-wide base sandbox image',
    NULL,
    '@base',
    1
);
