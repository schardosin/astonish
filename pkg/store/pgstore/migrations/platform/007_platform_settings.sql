-- Platform-wide settings (provider configuration, defaults).
-- Key-value design allows multiple setting categories without schema changes.
-- Secrets (api_key, client_secret) are stored encrypted using the platform master key.
CREATE TABLE IF NOT EXISTS platform_settings (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
