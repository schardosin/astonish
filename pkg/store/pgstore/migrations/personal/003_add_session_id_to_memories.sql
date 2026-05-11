-- Add session_id to memories table to track which session created each memory.
-- This enables per-session memory listing and extraction workflows.
ALTER TABLE {{schema}}.memories ADD COLUMN IF NOT EXISTS session_id UUID;

-- Index for fast lookups by session
CREATE INDEX IF NOT EXISTS idx_memories_session_id ON {{schema}}.memories (session_id) WHERE session_id IS NOT NULL;
