-- v1alpha1 semantic-embedding columns.
--
-- Adds pgvector-backed embedding storage to every v1alpha1 kind. Columns
-- are additive on top of the schema already defined in 001_v1alpha1_schema.sql;
-- a fresh install applies 001 → 002 → 003 in order. Embedding generation and
-- semantic-search behavior are opt-in at runtime via
-- AGENT_REGISTRY_EMBEDDINGS_ENABLED; when disabled the columns stay NULL and
-- pgvector is still required only for the migration (the extension is a
-- hard prerequisite to satisfy the column type regardless of runtime state).
--
-- The vector dimension is fixed at 1536 to match OpenAI's
-- text-embedding-3-small default. Switching to a provider with a different
-- dimension requires a schema rewrite — document this in operator notes when
-- we add provider abstraction beyond OpenAI.
--
-- Indexing:
--   - HNSW index with vector_cosine_ops on each table for ANN search.
--   - The per-kind ?semantic=<q> list-endpoint path issues `ORDER BY
--     semantic_embedding <=> $1 LIMIT k` against this index.
--   - Rows with NULL semantic_embedding are skipped in semantic queries
--     (caller responsibility via IS NOT NULL predicate).

CREATE EXTENSION IF NOT EXISTS vector;

-- -----------------------------------------------------------------------------
-- Per-kind semantic_embedding columns.
-- Identical shape across every kind — keeps the generic Store methods
-- uniform.
-- -----------------------------------------------------------------------------

ALTER TABLE v1alpha1.agents
    ADD COLUMN semantic_embedding              vector(1536),
    ADD COLUMN semantic_embedding_provider     TEXT,
    ADD COLUMN semantic_embedding_model        TEXT,
    ADD COLUMN semantic_embedding_dimensions   INTEGER,
    ADD COLUMN semantic_embedding_checksum     TEXT,
    ADD COLUMN semantic_embedding_generated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS v1alpha1_agents_semantic_embedding_hnsw
    ON v1alpha1.agents USING hnsw (semantic_embedding vector_cosine_ops);

ALTER TABLE v1alpha1.mcp_servers
    ADD COLUMN semantic_embedding              vector(1536),
    ADD COLUMN semantic_embedding_provider     TEXT,
    ADD COLUMN semantic_embedding_model        TEXT,
    ADD COLUMN semantic_embedding_dimensions   INTEGER,
    ADD COLUMN semantic_embedding_checksum     TEXT,
    ADD COLUMN semantic_embedding_generated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_semantic_embedding_hnsw
    ON v1alpha1.mcp_servers USING hnsw (semantic_embedding vector_cosine_ops);

ALTER TABLE v1alpha1.skills
    ADD COLUMN semantic_embedding              vector(1536),
    ADD COLUMN semantic_embedding_provider     TEXT,
    ADD COLUMN semantic_embedding_model        TEXT,
    ADD COLUMN semantic_embedding_dimensions   INTEGER,
    ADD COLUMN semantic_embedding_checksum     TEXT,
    ADD COLUMN semantic_embedding_generated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS v1alpha1_skills_semantic_embedding_hnsw
    ON v1alpha1.skills USING hnsw (semantic_embedding vector_cosine_ops);

ALTER TABLE v1alpha1.prompts
    ADD COLUMN semantic_embedding              vector(1536),
    ADD COLUMN semantic_embedding_provider     TEXT,
    ADD COLUMN semantic_embedding_model        TEXT,
    ADD COLUMN semantic_embedding_dimensions   INTEGER,
    ADD COLUMN semantic_embedding_checksum     TEXT,
    ADD COLUMN semantic_embedding_generated_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS v1alpha1_prompts_semantic_embedding_hnsw
    ON v1alpha1.prompts USING hnsw (semantic_embedding vector_cosine_ops);

-- Providers + Deployments don't participate in semantic search today (users
-- search Agents/MCPServers/Skills/Prompts for capabilities; Providers are
-- infrastructure metadata and Deployments are lifecycle state). Columns
-- stay off these tables to keep the schema honest — add them here if the
-- search model changes.
