-- Migration 002: Multi-file skill support + validation status

CREATE TABLE IF NOT EXISTS org_skill_files (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    skill_id        UUID NOT NULL REFERENCES org_skills(id) ON DELETE CASCADE,
    path            TEXT NOT NULL DEFAULT '',
    filename        TEXT NOT NULL,
    content         TEXT NOT NULL,
    is_executable   BOOLEAN NOT NULL DEFAULT false,
    size_bytes      BIGINT NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(skill_id, path, filename)
);

CREATE INDEX IF NOT EXISTS org_skill_files_skill_id_idx ON org_skill_files(skill_id);
CREATE INDEX IF NOT EXISTS org_skill_files_path_idx ON org_skill_files(path);

-- Validation status tracking: skills start as 'unknown' and must be explicitly
-- validated before they can be used at runtime (security enforcement).
ALTER TABLE org_skills ADD COLUMN IF NOT EXISTS validation_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE org_skills ADD COLUMN IF NOT EXISTS validation_meta JSONB;
