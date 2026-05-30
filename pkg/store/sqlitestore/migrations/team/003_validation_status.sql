-- Add validation status tracking to skills table.
-- Skills start as 'unknown' (not validated) and must be explicitly validated
-- before they can be used at runtime (security enforcement).
ALTER TABLE skills ADD COLUMN validation_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE skills ADD COLUMN validation_meta TEXT;
