-- 002_stateless_sessions.sql
-- Adds PG-backed transient session tables for stateless horizontal scaling.
-- These replace in-memory stores that prevent multi-instance deployments.

-- Device sessions: transient SSO/OIDC device flow state.
-- Replaces the in-memory deviceSessionStore.
-- TTL: 10 minutes (cleaned up periodically).
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
CREATE INDEX IF NOT EXISTS idx_device_sessions_state ON device_sessions(state);
CREATE INDEX IF NOT EXISTS idx_device_sessions_expires ON device_sessions(expires_at);

-- Pending link codes: transient channel-linking verification codes.
-- Replaces the in-memory LinkCodeStore.
-- TTL: 5 minutes (cleaned up periodically).
CREATE TABLE IF NOT EXISTS pending_link_codes (
    code       CHAR(6) PRIMARY KEY,
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      TEXT,
    channel    TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL DEFAULT (now() + interval '5 minutes')
);
CREATE INDEX IF NOT EXISTS idx_pending_link_codes_user_channel ON pending_link_codes(user_id, channel);
CREATE INDEX IF NOT EXISTS idx_pending_link_codes_expires ON pending_link_codes(expires_at);
