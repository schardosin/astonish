-- Add full-text search support to team memories
-- tsvector column + GIN index for hybrid vector+keyword search

ALTER TABLE {{schema}}.memories
    ADD COLUMN IF NOT EXISTS tsv tsvector;

-- Populate tsvector for existing rows
UPDATE {{schema}}.memories
    SET tsv = to_tsvector('english', chunk_text)
    WHERE tsv IS NULL;

-- GIN index for fast full-text queries
CREATE INDEX IF NOT EXISTS idx_team_memories_tsv
    ON {{schema}}.memories USING GIN (tsv);

-- Trigger to auto-update tsvector on INSERT/UPDATE
CREATE OR REPLACE FUNCTION {{schema}}.memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('english', NEW.chunk_text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_memories_tsv ON {{schema}}.memories;
CREATE TRIGGER trg_memories_tsv
    BEFORE INSERT OR UPDATE OF chunk_text ON {{schema}}.memories
    FOR EACH ROW EXECUTE FUNCTION {{schema}}.memories_tsv_trigger();
