-- Add full-text search support to org memories
-- tsvector column + GIN index for hybrid vector+keyword search

ALTER TABLE org_memories
    ADD COLUMN IF NOT EXISTS tsv tsvector;

-- Populate tsvector for existing rows
UPDATE org_memories
    SET tsv = to_tsvector('english', chunk_text)
    WHERE tsv IS NULL;

-- GIN index for fast full-text queries
CREATE INDEX IF NOT EXISTS idx_org_memories_tsv
    ON org_memories USING GIN (tsv);

-- Trigger to auto-update tsvector on INSERT/UPDATE
CREATE OR REPLACE FUNCTION org_memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.tsv := to_tsvector('english', NEW.chunk_text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_org_memories_tsv ON org_memories;
CREATE TRIGGER trg_org_memories_tsv
    BEFORE INSERT OR UPDATE OF chunk_text ON org_memories
    FOR EACH ROW EXECUTE FUNCTION org_memories_tsv_trigger();
