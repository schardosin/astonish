-- Personal schema (SQLite) — desired state.
-- Source of truth for Atlas diff generation.
-- Contains: user-private memories, apps, sessions, flows, credentials.

CREATE TABLE IF NOT EXISTS memories (
    id TEXT PRIMARY KEY,
    chunk_text TEXT NOT NULL,
    embedding BLOB,
    category TEXT,
    source_path TEXT,
    metadata TEXT,
    created_by TEXT,
    session_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS memories_fts USING fts5(
    chunk_text,
    content='memories',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS memories_ai AFTER INSERT ON memories BEGIN
    INSERT INTO memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
END;
CREATE TRIGGER IF NOT EXISTS memories_ad AFTER DELETE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
END;
CREATE TRIGGER IF NOT EXISTS memories_au AFTER UPDATE ON memories BEGIN
    INSERT INTO memories_fts(memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
    INSERT INTO memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
END;

CREATE TABLE IF NOT EXISTS apps (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    code TEXT,
    version INTEGER DEFAULT 1,
    session_id TEXT,
    data_sources TEXT DEFAULT '[]',
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS app_state (
    app_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (app_id, key)
);

CREATE TABLE IF NOT EXISTS sessions (
    id TEXT PRIMARY KEY,
    user_id TEXT NOT NULL DEFAULT '',
    title TEXT,
    message_count INTEGER DEFAULT 0,
    parent_id TEXT,
    fleet_key TEXT DEFAULT '',
    fleet_name TEXT DEFAULT '',
    issue_number INTEGER,
    repo TEXT,
    workspace_dir TEXT DEFAULT '',
    metadata TEXT DEFAULT '{}',
    last_seq INTEGER DEFAULT 0,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS session_events (
    id INTEGER PRIMARY KEY,
    session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    event_data TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_session_events_session ON session_events(session_id);

CREATE TABLE IF NOT EXISTS flows (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    definition TEXT,
    yaml_content TEXT,
    type TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS credentials (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    cred_type TEXT NOT NULL,
    encrypted BLOB NOT NULL,
    created_by TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
