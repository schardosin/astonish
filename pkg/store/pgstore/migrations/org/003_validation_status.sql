-- Add validation status tracking to org_skills table.
-- Skills start as 'unknown' (not validated) and must be explicitly validated
-- before they can be used at runtime (security enforcement).
ALTER TABLE org_skills ADD COLUMN IF NOT EXISTS validation_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE org_skills ADD COLUMN IF NOT EXISTS validation_meta JSONB;
