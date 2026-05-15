-- Drop the v1alpha1 semantic-embedding columns + HNSW indexes left over
-- from the removed semantic-search feature. The pgvector extension is
-- intentionally left installed: extensions are database-global and may
-- be in use by downstream schemas.
--
-- Idempotent — every statement uses IF EXISTS so this is safe on
-- installs that ran with AGENT_REGISTRY_EMBEDDINGS_ENABLED=false (the
-- columns/indexes never existed) and on installs that ran with it
-- true (003_embeddings.sql had created them).

DROP INDEX IF EXISTS v1alpha1.v1alpha1_agents_semantic_embedding_hnsw;
DROP INDEX IF EXISTS v1alpha1.v1alpha1_mcp_servers_semantic_embedding_hnsw;
DROP INDEX IF EXISTS v1alpha1.v1alpha1_skills_semantic_embedding_hnsw;
DROP INDEX IF EXISTS v1alpha1.v1alpha1_prompts_semantic_embedding_hnsw;

ALTER TABLE v1alpha1.agents
    DROP COLUMN IF EXISTS semantic_embedding,
    DROP COLUMN IF EXISTS semantic_embedding_provider,
    DROP COLUMN IF EXISTS semantic_embedding_model,
    DROP COLUMN IF EXISTS semantic_embedding_dimensions,
    DROP COLUMN IF EXISTS semantic_embedding_checksum,
    DROP COLUMN IF EXISTS semantic_embedding_generated_at;

ALTER TABLE v1alpha1.mcp_servers
    DROP COLUMN IF EXISTS semantic_embedding,
    DROP COLUMN IF EXISTS semantic_embedding_provider,
    DROP COLUMN IF EXISTS semantic_embedding_model,
    DROP COLUMN IF EXISTS semantic_embedding_dimensions,
    DROP COLUMN IF EXISTS semantic_embedding_checksum,
    DROP COLUMN IF EXISTS semantic_embedding_generated_at;

ALTER TABLE v1alpha1.skills
    DROP COLUMN IF EXISTS semantic_embedding,
    DROP COLUMN IF EXISTS semantic_embedding_provider,
    DROP COLUMN IF EXISTS semantic_embedding_model,
    DROP COLUMN IF EXISTS semantic_embedding_dimensions,
    DROP COLUMN IF EXISTS semantic_embedding_checksum,
    DROP COLUMN IF EXISTS semantic_embedding_generated_at;

ALTER TABLE v1alpha1.prompts
    DROP COLUMN IF EXISTS semantic_embedding,
    DROP COLUMN IF EXISTS semantic_embedding_provider,
    DROP COLUMN IF EXISTS semantic_embedding_model,
    DROP COLUMN IF EXISTS semantic_embedding_dimensions,
    DROP COLUMN IF EXISTS semantic_embedding_checksum,
    DROP COLUMN IF EXISTS semantic_embedding_generated_at;
