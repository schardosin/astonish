-- Email thread index: maps email Message-ID headers to session keys.
-- Enables per-thread email sessions where replies route to the same
-- session as the original message (via In-Reply-To / References headers).
-- Platform-level table: one email bot per daemon, handles all orgs/teams.
CREATE TABLE IF NOT EXISTS email_thread_index (
    message_id  TEXT PRIMARY KEY,
    session_key TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Index for reverse lookups (e.g., removing all entries for a deleted session)
CREATE INDEX IF NOT EXISTS idx_email_thread_index_session
    ON email_thread_index (session_key);
