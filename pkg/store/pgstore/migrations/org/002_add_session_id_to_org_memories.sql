-- Add session_id to org_memories table to track which session created each memory.
ALTER TABLE org_memories ADD COLUMN IF NOT EXISTS session_id UUID;

-- Index for fast lookups by session
CREATE INDEX IF NOT EXISTS idx_org_memories_session_id ON org_memories (session_id) WHERE session_id IS NOT NULL;
