-- Create agents table mirroring servers structure
-- Each row represents a specific version of an agent identified by (agent_name, version)

CREATE TABLE IF NOT EXISTS agents (
    agent_name    VARCHAR(255) NOT NULL,
    version       VARCHAR(255) NOT NULL,
    status        VARCHAR(50)  NOT NULL DEFAULT 'active',
    published_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_latest     BOOLEAN NOT NULL DEFAULT true,

    -- Complete AgentJSON payload as JSONB (same shape as ServerJSON for now)
    value         JSONB NOT NULL,

    CONSTRAINT agents_pkey PRIMARY KEY (agent_name, version)
);

-- Indexes to mirror servers performance characteristics
CREATE INDEX IF NOT EXISTS idx_agents_name ON agents (agent_name);
CREATE INDEX IF NOT EXISTS idx_agents_name_version ON agents (agent_name, version);
CREATE INDEX IF NOT EXISTS idx_agents_latest ON agents (agent_name, is_latest) WHERE is_latest = true;
CREATE INDEX IF NOT EXISTS idx_agents_status ON agents (status);
CREATE INDEX IF NOT EXISTS idx_agents_published_at ON agents (published_at DESC);
CREATE INDEX IF NOT EXISTS idx_agents_updated_at ON agents (updated_at DESC);

-- Trigger and function to auto-update updated_at on modification
CREATE OR REPLACE FUNCTION update_agents_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_update_agents_updated_at ON agents;
CREATE TRIGGER trg_update_agents_updated_at
    BEFORE UPDATE ON agents
    FOR EACH ROW
    EXECUTE FUNCTION update_agents_updated_at();

-- Ensure only one version per agent is marked latest
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_latest_per_agent
ON agents (agent_name)
WHERE is_latest = true;

-- Basic integrity checks similar to servers
ALTER TABLE agents ADD CONSTRAINT check_agent_status_valid
CHECK (status IN ('active', 'deprecated', 'deleted'));

ALTER TABLE agents ADD CONSTRAINT check_agent_name_format
CHECK (agent_name ~ '^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$');

ALTER TABLE agents ADD CONSTRAINT check_agent_version_not_empty
CHECK (length(trim(version)) > 0);

