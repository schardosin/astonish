-- Migration 002: Add org_skill_files table for multi-file skill support (org level)

CREATE TABLE IF NOT EXISTS org_skill_files (
    id            TEXT PRIMARY KEY,
    skill_id      TEXT NOT NULL REFERENCES org_skills(id) ON DELETE CASCADE,
    path          TEXT NOT NULL DEFAULT '',
    filename      TEXT NOT NULL,
    content       TEXT NOT NULL,
    is_executable BOOLEAN NOT NULL DEFAULT 0,
    size_bytes    INTEGER NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(skill_id, path, filename)
);

CREATE INDEX IF NOT EXISTS idx_org_skill_files_skill_id ON org_skill_files(skill_id);
CREATE INDEX IF NOT EXISTS idx_org_skill_files_path ON org_skill_files(path);
