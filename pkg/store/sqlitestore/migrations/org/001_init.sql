-- Organization database: teams, memberships, org-level memories/skills/apps/audit.

CREATE TABLE IF NOT EXISTS teams (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    slug TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    settings TEXT DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS team_memberships (
    user_id TEXT NOT NULL,
    team_id TEXT NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    role TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('admin','member','viewer')),
    joined_at TEXT NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (user_id, team_id)
);

CREATE TABLE IF NOT EXISTS org_memories (
    id TEXT PRIMARY KEY,
    chunk_text TEXT NOT NULL,
    embedding BLOB,
    category TEXT,
    source_path TEXT,
    metadata TEXT,
    promoted_by TEXT,
    promoted_from_team TEXT,
    session_id TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE VIRTUAL TABLE IF NOT EXISTS org_memories_fts USING fts5(
    chunk_text,
    content='org_memories',
    content_rowid='rowid'
);

CREATE TRIGGER IF NOT EXISTS org_memories_ai AFTER INSERT ON org_memories BEGIN
    INSERT INTO org_memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
END;
CREATE TRIGGER IF NOT EXISTS org_memories_ad AFTER DELETE ON org_memories BEGIN
    INSERT INTO org_memories_fts(org_memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
END;
CREATE TRIGGER IF NOT EXISTS org_memories_au AFTER UPDATE ON org_memories BEGIN
    INSERT INTO org_memories_fts(org_memories_fts, rowid, chunk_text) VALUES('delete', old.rowid, old.chunk_text);
    INSERT INTO org_memories_fts(rowid, chunk_text) VALUES (new.rowid, new.chunk_text);
END;

CREATE TABLE IF NOT EXISTS org_skills (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    content TEXT NOT NULL,
    frontmatter TEXT,
    created_by TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS org_mcp_servers (
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

CREATE TABLE IF NOT EXISTS org_apps (
    id TEXT PRIMARY KEY,
    slug TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    description TEXT,
    definition TEXT NOT NULL,
    promoted_by TEXT,
    promoted_from_team TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS org_audit_log (
    id INTEGER PRIMARY KEY,
    timestamp TEXT NOT NULL DEFAULT (datetime('now')),
    user_id TEXT NOT NULL,
    team_id TEXT,
    action TEXT NOT NULL,
    resource TEXT NOT NULL,
    detail TEXT,
    ip_address TEXT,
    session_id TEXT
);

CREATE TABLE IF NOT EXISTS org_encryption_keys (
    id TEXT PRIMARY KEY,
    key_name TEXT NOT NULL UNIQUE,
    key_data BLOB NOT NULL,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
