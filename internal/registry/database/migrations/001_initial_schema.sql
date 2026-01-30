-- Initial schema for Agent Registry
-- This consolidated migration creates all tables, indexes, constraints, and triggers

-- =============================================================================
-- EXTENSIONS
-- =============================================================================

-- UUID generation support
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Trigram support for text search
CREATE EXTENSION IF NOT EXISTS "pg_trgm";

-- pgvector for semantic embeddings
CREATE EXTENSION IF NOT EXISTS vector;

-- =============================================================================
-- SERVERS TABLE
-- =============================================================================

CREATE TABLE servers (
    -- Primary identifiers
    server_name VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    
    -- Status and timestamps
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    published_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_latest BOOLEAN NOT NULL DEFAULT true,
    
    -- Complete ServerJSON payload as JSONB
    value JSONB NOT NULL,
    
    -- Publishing state
    published BOOLEAN NOT NULL DEFAULT false,
    published_date TIMESTAMP WITH TIME ZONE,
    unpublished_date TIMESTAMP WITH TIME ZONE,
    
    -- Semantic embedding columns for vector search
    semantic_embedding vector(1536),
    semantic_embedding_provider TEXT,
    semantic_embedding_model TEXT,
    semantic_embedding_dimensions INTEGER,
    semantic_embedding_checksum TEXT,
    semantic_embedding_generated_at TIMESTAMPTZ,
    
    -- Primary key
    CONSTRAINT servers_pkey PRIMARY KEY (server_name, version)
);

-- Indexes for servers
CREATE INDEX idx_servers_name ON servers (server_name);
CREATE INDEX idx_servers_name_version ON servers (server_name, version);
CREATE INDEX idx_servers_name_latest ON servers (server_name, is_latest) WHERE is_latest = true;
CREATE INDEX idx_servers_status ON servers (status);
CREATE INDEX idx_servers_published_at ON servers (published_at DESC);
CREATE INDEX idx_servers_updated_at ON servers (updated_at DESC);
CREATE INDEX idx_servers_published ON servers (published);

-- Ensure only one version per server is marked as latest
CREATE UNIQUE INDEX idx_unique_latest_per_server ON servers (server_name) WHERE is_latest = true;

-- GIN indexes for JSONB queries
CREATE INDEX idx_servers_json_remotes ON servers USING GIN((value->'remotes'));
CREATE INDEX idx_servers_json_packages ON servers USING GIN((value->'packages'));

-- HNSW index for semantic embedding similarity search
CREATE INDEX idx_servers_semantic_embedding_hnsw ON servers USING hnsw (semantic_embedding vector_cosine_ops);

-- Check constraints for servers
ALTER TABLE servers ADD CONSTRAINT check_status_valid
    CHECK (status IN ('active', 'deprecated', 'deleted'));

ALTER TABLE servers ADD CONSTRAINT check_server_name_format
    CHECK (server_name ~ '^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$');

ALTER TABLE servers ADD CONSTRAINT check_version_not_empty
    CHECK (length(trim(version)) > 0);

ALTER TABLE servers ADD CONSTRAINT check_published_at_reasonable
    CHECK (published_at >= '2020-01-01'::timestamp AND published_at <= NOW() + interval '1 day');

-- Trigger function to auto-update updated_at
CREATE OR REPLACE FUNCTION update_servers_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_update_servers_updated_at
    BEFORE UPDATE ON servers
    FOR EACH ROW
    EXECUTE FUNCTION update_servers_updated_at();

-- =============================================================================
-- SKILLS TABLE
-- =============================================================================

CREATE TABLE skills (
    -- Primary identifiers
    skill_name VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    
    -- Status and timestamps
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    published_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_latest BOOLEAN NOT NULL DEFAULT true,
    
    -- Complete SkillJSON payload as JSONB
    value JSONB NOT NULL,
    
    -- Publishing state
    published BOOLEAN NOT NULL DEFAULT false,
    published_date TIMESTAMP WITH TIME ZONE,
    unpublished_date TIMESTAMP WITH TIME ZONE,
    
    -- Primary key
    CONSTRAINT skills_pkey PRIMARY KEY (skill_name, version)
);

-- Indexes for skills
CREATE INDEX idx_skills_name ON skills (skill_name);
CREATE INDEX idx_skills_name_version ON skills (skill_name, version);
CREATE INDEX idx_skills_latest ON skills (skill_name, is_latest) WHERE is_latest = true;
CREATE INDEX idx_skills_status ON skills (status);
CREATE INDEX idx_skills_published_at ON skills (published_at DESC);
CREATE INDEX idx_skills_updated_at ON skills (updated_at DESC);
CREATE INDEX idx_skills_published ON skills (published);

-- Ensure only one version per skill is marked as latest
CREATE UNIQUE INDEX idx_unique_latest_per_skill ON skills (skill_name) WHERE is_latest = true;

-- Trigger function to auto-update updated_at
CREATE OR REPLACE FUNCTION update_skills_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_update_skills_updated_at
    BEFORE UPDATE ON skills
    FOR EACH ROW
    EXECUTE FUNCTION update_skills_updated_at();

-- Check constraints for skills
ALTER TABLE skills ADD CONSTRAINT check_skill_status_valid
    CHECK (status IN ('active', 'deprecated', 'deleted'));

ALTER TABLE skills ADD CONSTRAINT check_skill_name_format
    CHECK (skill_name ~ '^[a-zA-Z0-9_-]+$');

ALTER TABLE skills ADD CONSTRAINT check_skill_version_not_empty
    CHECK (length(trim(version)) > 0);

-- =============================================================================
-- AGENTS TABLE
-- =============================================================================

CREATE TABLE agents (
    -- Primary identifiers
    agent_name VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    
    -- Status and timestamps
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    published_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_latest BOOLEAN NOT NULL DEFAULT true,
    
    -- Complete AgentJSON payload as JSONB
    value JSONB NOT NULL,
    
    -- Publishing state
    published BOOLEAN NOT NULL DEFAULT false,
    published_date TIMESTAMP WITH TIME ZONE,
    unpublished_date TIMESTAMP WITH TIME ZONE,
    
    -- Semantic embedding columns for vector search
    semantic_embedding vector(1536),
    semantic_embedding_provider TEXT,
    semantic_embedding_model TEXT,
    semantic_embedding_dimensions INTEGER,
    semantic_embedding_checksum TEXT,
    semantic_embedding_generated_at TIMESTAMPTZ,
    
    -- Primary key
    CONSTRAINT agents_pkey PRIMARY KEY (agent_name, version)
);

-- Indexes for agents
CREATE INDEX idx_agents_name ON agents (agent_name);
CREATE INDEX idx_agents_name_version ON agents (agent_name, version);
CREATE INDEX idx_agents_latest ON agents (agent_name, is_latest) WHERE is_latest = true;
CREATE INDEX idx_agents_status ON agents (status);
CREATE INDEX idx_agents_published_at ON agents (published_at DESC);
CREATE INDEX idx_agents_updated_at ON agents (updated_at DESC);
CREATE INDEX idx_agents_published ON agents (published);

-- Ensure only one version per agent is marked as latest
CREATE UNIQUE INDEX idx_unique_latest_per_agent ON agents (agent_name) WHERE is_latest = true;

-- HNSW index for semantic embedding similarity search
CREATE INDEX idx_agents_semantic_embedding_hnsw ON agents USING hnsw (semantic_embedding vector_cosine_ops);

-- Trigger function to auto-update updated_at
CREATE OR REPLACE FUNCTION update_agents_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_update_agents_updated_at
    BEFORE UPDATE ON agents
    FOR EACH ROW
    EXECUTE FUNCTION update_agents_updated_at();

-- Check constraints for agents
ALTER TABLE agents ADD CONSTRAINT check_agent_status_valid
    CHECK (status IN ('active', 'deprecated', 'deleted'));

ALTER TABLE agents ADD CONSTRAINT check_agent_name_format
    CHECK (agent_name ~ '^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$');

ALTER TABLE agents ADD CONSTRAINT check_agent_version_not_empty
    CHECK (length(trim(version)) > 0);

-- =============================================================================
-- DEPLOYMENTS TABLE
-- =============================================================================

CREATE TABLE deployments (
    -- Primary identifiers
    server_name VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    
    -- Timestamps
    deployed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Status and configuration
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    config JSONB DEFAULT '{}'::jsonb,
    prefer_remote BOOLEAN DEFAULT false,
    
    -- Resource type (mcp server or agent)
    resource_type VARCHAR(50) NOT NULL DEFAULT 'mcp',
    
    -- Runtime target
    runtime VARCHAR(50) NOT NULL DEFAULT 'local',
    
    -- Primary key
    CONSTRAINT deployments_pkey PRIMARY KEY (server_name, version)
);

-- Indexes for deployments
CREATE INDEX idx_deployments_server_name ON deployments (server_name);
CREATE INDEX idx_deployments_status ON deployments (status);
CREATE INDEX idx_deployments_deployed_at ON deployments (deployed_at DESC);
CREATE INDEX idx_deployments_updated_at ON deployments (updated_at DESC);
CREATE INDEX idx_deployments_resource_type ON deployments (resource_type);
CREATE INDEX idx_deployments_runtime ON deployments (runtime);

-- GIN index for config JSONB queries
CREATE INDEX idx_deployments_config_gin ON deployments USING GIN(config);

-- Trigger function to auto-update updated_at
CREATE OR REPLACE FUNCTION update_deployments_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_update_deployments_updated_at
    BEFORE UPDATE ON deployments
    FOR EACH ROW
    EXECUTE FUNCTION update_deployments_updated_at();

-- Check constraints for deployments
ALTER TABLE deployments ADD CONSTRAINT check_deployment_status_valid
    CHECK (status IN ('active', 'stopped', 'failed'));

ALTER TABLE deployments ADD CONSTRAINT check_deployment_server_name_not_empty
    CHECK (length(trim(server_name)) > 0);

ALTER TABLE deployments ADD CONSTRAINT check_deployment_version_not_empty
    CHECK (length(trim(version)) > 0);

ALTER TABLE deployments ADD CONSTRAINT check_deployment_resource_type_valid
    CHECK (resource_type IN ('mcp', 'agent'));

ALTER TABLE deployments ADD CONSTRAINT check_deployment_runtime_valid
    CHECK (runtime IN ('local', 'kubernetes'));

-- Add comments for documentation
COMMENT ON COLUMN deployments.resource_type IS 'Type of resource deployed: mcp (MCP server) or agent';
COMMENT ON COLUMN deployments.runtime IS 'Deployment runtime target: local or kubernetes';

-- =============================================================================
-- SERVER_READMES TABLE
-- =============================================================================

CREATE TABLE server_readmes (
    -- Primary identifiers (references servers table)
    server_name VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    
    -- Content
    content BYTEA NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'text/markdown',
    size_bytes INTEGER NOT NULL,
    sha256 BYTEA NOT NULL,
    fetched_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    
    -- Primary key
    PRIMARY KEY (server_name, version),
    
    -- Foreign key reference to servers
    CONSTRAINT fk_server_readmes_server FOREIGN KEY (server_name, version)
        REFERENCES servers(server_name, version)
        ON DELETE CASCADE
);

-- Index for server_readmes
CREATE INDEX idx_server_readmes_server_name_version ON server_readmes (server_name, version);
