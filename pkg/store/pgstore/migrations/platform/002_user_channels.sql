-- user_channels: Links platform users to external messaging channels.
-- A user can have multiple channel links (e.g., Telegram + Email).
-- The daemon uses this table to:
--   1. Build the dynamic allowlist for Telegram (replacing static config)
--   2. Route inbound messages to the correct user/team context
--   3. Resolve delivery targets for scheduler job results
CREATE TABLE IF NOT EXISTS user_channels (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    channel_type VARCHAR(32) NOT NULL,        -- 'telegram', 'email'
    external_id VARCHAR(255) NOT NULL,        -- TG user ID (numeric string), or email address
    display_name VARCHAR(255) DEFAULT '',     -- @username or email label
    default_org_slug VARCHAR(100) DEFAULT '', -- preferred org for inbound routing
    default_team_slug VARCHAR(100) DEFAULT '',-- preferred team for inbound routing
    enabled BOOLEAN DEFAULT true,
    verified BOOLEAN DEFAULT false,           -- true after verification handshake
    verified_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    UNIQUE(channel_type, external_id)
);

CREATE INDEX IF NOT EXISTS idx_user_channels_user ON user_channels(user_id);
CREATE INDEX IF NOT EXISTS idx_user_channels_lookup ON user_channels(channel_type, external_id);
CREATE INDEX IF NOT EXISTS idx_user_channels_type_enabled ON user_channels(channel_type, enabled, verified);
