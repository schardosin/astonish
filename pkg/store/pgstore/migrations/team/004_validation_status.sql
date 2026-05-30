-- Add validation status tracking to skills table.
-- Skills start as 'unknown' (not validated) and must be explicitly validated
-- before they can be used at runtime (security enforcement).
ALTER TABLE {{schema}}.skills ADD COLUMN IF NOT EXISTS validation_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE {{schema}}.skills ADD COLUMN IF NOT EXISTS validation_meta JSONB;
