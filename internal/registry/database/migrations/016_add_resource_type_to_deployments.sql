-- Add resource_type column to deployments table for future support of deploying agents
-- Currently defaults to 'mcp' for existing and new deployments

ALTER TABLE deployments 
ADD COLUMN IF NOT EXISTS resource_type VARCHAR(50) NOT NULL DEFAULT 'mcp';

-- Add constraint to ensure valid resource types
ALTER TABLE deployments 
ADD CONSTRAINT check_deployment_resource_type_valid
CHECK (resource_type IN ('mcp', 'agent'));

-- Create index for filtering by resource type
CREATE INDEX IF NOT EXISTS idx_deployments_resource_type ON deployments (resource_type);

-- Add comment for documentation
COMMENT ON COLUMN deployments.resource_type IS 'Type of resource deployed: mcp (MCP server) or agent';

