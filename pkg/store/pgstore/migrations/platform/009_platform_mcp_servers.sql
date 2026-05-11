-- Platform-level MCP servers: inherited by all organizations and teams.
-- Managed by superadmins via the Platform MCP settings tab.
-- Env values are encrypted at the application layer (AES-256-GCM).
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
