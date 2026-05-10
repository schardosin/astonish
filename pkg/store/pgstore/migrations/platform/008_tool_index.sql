-- Tool index for semantic tool discovery (platform mode).
-- Stores tool name + description with their vector embeddings.
-- Used by the ToolIndex to find relevant tools for a user's request
-- via pgvector cosine similarity search.
CREATE TABLE IF NOT EXISTS tool_index (
    id         TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    embedding  vector(384),
    metadata   JSONB NOT NULL DEFAULT '{}',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_tool_index_embedding
    ON tool_index USING hnsw (embedding vector_cosine_ops);
