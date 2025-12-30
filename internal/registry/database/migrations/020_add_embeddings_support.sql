-- Enable pgvector extension and add embedding columns for semantic search

-- pgvector extension is required for vector similarity search
CREATE EXTENSION IF NOT EXISTS vector;

-- Servers table embedding metadata
ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS semantic_embedding vector,
    ADD COLUMN IF NOT EXISTS semantic_embedding_provider TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_model TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_dimensions INTEGER,
    ADD COLUMN IF NOT EXISTS semantic_embedding_checksum TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_generated_at TIMESTAMPTZ;

-- Agents table embedding metadata
ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS semantic_embedding vector,
    ADD COLUMN IF NOT EXISTS semantic_embedding_provider TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_model TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_dimensions INTEGER,
    ADD COLUMN IF NOT EXISTS semantic_embedding_checksum TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_generated_at TIMESTAMPTZ;

-- Indexes to accelerate approximate nearest neighbor search
-- HNSW is the default choice for balanced recall and latency
CREATE INDEX IF NOT EXISTS idx_servers_semantic_embedding_hnsw
    ON servers USING hnsw (semantic_embedding vector_cosine_ops);

CREATE INDEX IF NOT EXISTS idx_agents_semantic_embedding_hnsw
    ON agents USING hnsw (semantic_embedding vector_cosine_ops);

