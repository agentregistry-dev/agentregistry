-- Create deployments table to track which servers are deployed
-- Each deployment represents a server that has been explicitly deployed by the user

CREATE TABLE IF NOT EXISTS deployments (
    server_name    VARCHAR(255) NOT NULL,
    version        VARCHAR(255) NOT NULL,
    deployed_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    status         VARCHAR(50) NOT NULL DEFAULT 'active',
    
    -- Configuration stored as JSONB (env vars, args, headers)
    config         JSONB DEFAULT '{}'::jsonb,
    
    -- Preference for remote vs local deployment
    prefer_remote  BOOLEAN DEFAULT false,
    
    CONSTRAINT deployments_pkey PRIMARY KEY (server_name)
);

-- Indexes for performance
CREATE INDEX IF NOT EXISTS idx_deployments_server_name ON deployments (server_name);
CREATE INDEX IF NOT EXISTS idx_deployments_status ON deployments (status);
CREATE INDEX IF NOT EXISTS idx_deployments_deployed_at ON deployments (deployed_at DESC);
CREATE INDEX IF NOT EXISTS idx_deployments_updated_at ON deployments (updated_at DESC);

-- GIN index for config JSONB queries
CREATE INDEX IF NOT EXISTS idx_deployments_config_gin ON deployments USING GIN(config);

-- Trigger to auto-update updated_at on modification
CREATE OR REPLACE FUNCTION update_deployments_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_update_deployments_updated_at ON deployments;
CREATE TRIGGER trg_update_deployments_updated_at
    BEFORE UPDATE ON deployments
    FOR EACH ROW
    EXECUTE FUNCTION update_deployments_updated_at();

-- Basic integrity checks
ALTER TABLE deployments ADD CONSTRAINT check_deployment_status_valid
CHECK (status IN ('active', 'stopped', 'failed'));

ALTER TABLE deployments ADD CONSTRAINT check_deployment_server_name_not_empty
CHECK (length(trim(server_name)) > 0);

ALTER TABLE deployments ADD CONSTRAINT check_deployment_version_not_empty
CHECK (length(trim(version)) > 0);

