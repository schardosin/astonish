-- 002_sandbox_sessions_and_events.sql
-- Phase A (Round 2): team-scoped tables for sandbox session metadata and
-- the cross-pod chat event journal.
--
-- Applied to: astonish_org_{slug} database, team_{team_slug} schema.
-- {{schema}} is replaced at migration time.
--
-- See docs/architecture/sandbox-backends.md:
--   §7     -- schema placement rationale
--   §5.14  -- cross-pod continuity / event journal
--
-- Personal mode never applies this migration.

-- ----------------------------------------------------------------------------
-- sandbox_sessions: team-scoped metadata for a running sandbox (tied to a
-- chat session). Tracks the evicted-upper layer when a session is paused.
--
-- upper_layer_id REFERENCES platform.sandbox_layers but cross-database FKs
-- aren't enforceable in PG; we store the TEXT layer_id and rely on the
-- application's ref-count discipline (§5.12). A NULL upper_layer_id means
-- the session has never been evicted (running or torn down without save).
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS {{schema}}.sandbox_sessions (
    id                 TEXT PRIMARY KEY,                       -- sandbox session UUID
    chat_session_id    TEXT NOT NULL,                          -- FK-shaped but not enforced; chat sessions live in {{schema}}.sessions
    template_id        UUID NOT NULL,                          -- platform.sandbox_templates.id (cross-db, not enforced)
    upper_layer_id     TEXT,                                   -- platform.sandbox_layers.layer_id when evicted; NULL while running
    state              TEXT NOT NULL DEFAULT 'creating'
                       CHECK (state IN ('creating', 'running', 'evicting', 'evicted', 'resuming', 'terminated')),
    pod_name           TEXT,                                   -- active pod, if running on K8s backend
    node_name          TEXT,
    created_by         UUID,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_chat
    ON {{schema}}.sandbox_sessions(chat_session_id);

CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_state_active
    ON {{schema}}.sandbox_sessions(state, last_active_at)
    WHERE state IN ('running', 'evicted');

CREATE INDEX IF NOT EXISTS idx_sandbox_sessions_upper_layer
    ON {{schema}}.sandbox_sessions(upper_layer_id)
    WHERE upper_layer_id IS NOT NULL;

-- ----------------------------------------------------------------------------
-- chat_session_events: append-only event journal for cross-pod continuity.
-- Producer holds a per-chat advisory lock; consumers replay from (session, seq).
-- Retention: ON DELETE CASCADE from sessions (§5.14).
-- ----------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS {{schema}}.chat_session_events (
    chat_session_id  TEXT NOT NULL REFERENCES {{schema}}.sessions(id) ON DELETE CASCADE,
    seq              BIGINT NOT NULL,
    event_type       TEXT NOT NULL,
    payload          JSONB NOT NULL,
    producer_pod     TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (chat_session_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_chat_session_events_created
    ON {{schema}}.chat_session_events(chat_session_id, created_at);

-- last_seq column on sessions: tracks the highest assigned seq per chat.
-- Allocation is a single UPDATE ... RETURNING in the same transaction as
-- the INSERT into chat_session_events (serialized by the advisory lock).
ALTER TABLE {{schema}}.sessions
    ADD COLUMN IF NOT EXISTS last_seq BIGINT NOT NULL DEFAULT 0;
