-- Migration 003: Add skill_files table for multi-file skill support (team schema)

CREATE TABLE IF NOT EXISTS {{schema}}.skill_files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id        UUID NOT NULL REFERENCES {{schema}}.skills(id) ON DELETE CASCADE,
    path            TEXT NOT NULL DEFAULT '',
    filename        TEXT NOT NULL,
    content         TEXT NOT NULL,
    is_executable   BOOLEAN NOT NULL DEFAULT false,
    size_bytes      BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(skill_id, path, filename)
);

CREATE INDEX IF NOT EXISTS skill_files_skill_id_idx ON {{schema}}.skill_files(skill_id);
CREATE INDEX IF NOT EXISTS skill_files_path_idx ON {{schema}}.skill_files(path);
