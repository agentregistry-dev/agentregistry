-- Vector extension and schema additions for semantic search.
-- Applied only when database.postgres.vectorEnabled=true (AGENT_REGISTRY_DATABASE_VECTOR_ENABLED=true).

CREATE EXTENSION IF NOT EXISTS vector;

ALTER TABLE servers
    ADD COLUMN IF NOT EXISTS semantic_embedding vector(1536),
    ADD COLUMN IF NOT EXISTS semantic_embedding_provider TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_model TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_dimensions INTEGER,
    ADD COLUMN IF NOT EXISTS semantic_embedding_checksum TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_generated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_servers_semantic_embedding_hnsw ON servers USING hnsw (semantic_embedding vector_cosine_ops);

ALTER TABLE agents
    ADD COLUMN IF NOT EXISTS semantic_embedding vector(1536),
    ADD COLUMN IF NOT EXISTS semantic_embedding_provider TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_model TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_dimensions INTEGER,
    ADD COLUMN IF NOT EXISTS semantic_embedding_checksum TEXT,
    ADD COLUMN IF NOT EXISTS semantic_embedding_generated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_agents_semantic_embedding_hnsw ON agents USING hnsw (semantic_embedding vector_cosine_ops);
