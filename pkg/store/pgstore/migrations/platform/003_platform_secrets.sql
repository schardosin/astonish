-- Platform-level secrets: instance-wide secrets not scoped to any org/team.
-- Examples: Telegram bot token, email channel password, web search API keys.
-- Encrypted with the master key directly (no per-org DEK indirection).
CREATE TABLE IF NOT EXISTS platform_secrets (
    key         TEXT PRIMARY KEY,
    value       BYTEA NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
