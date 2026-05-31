-- Migration 002: Add skill_files table for multi-file skill support
-- This enables storing auxiliary files (scripts/, references/, templates/, etc.)
-- alongside the main SKILL.md for a skill.

CREATE TABLE IF NOT EXISTS skill_files (
    id            TEXT PRIMARY KEY,
    skill_id      TEXT NOT NULL REFERENCES skills(id) ON DELETE CASCADE,
    path          TEXT NOT NULL DEFAULT '',
    filename      TEXT NOT NULL,
    content       TEXT NOT NULL,
    is_executable BOOLEAN NOT NULL DEFAULT 0,
    size_bytes    INTEGER NOT NULL,
    created_at    TEXT NOT NULL DEFAULT (datetime('now')),
    updated_at    TEXT NOT NULL DEFAULT (datetime('now')),
    UNIQUE(skill_id, path, filename)
);

CREATE INDEX IF NOT EXISTS idx_skill_files_skill_id ON skill_files(skill_id);
CREATE INDEX IF NOT EXISTS idx_skill_files_path ON skill_files(path);
