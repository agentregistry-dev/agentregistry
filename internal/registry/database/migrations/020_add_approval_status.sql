-- Add approval_status columns to servers, agents, and skills tables
-- approval_status can be: PENDING (default), APPROVED, or DENIED

-- Add approval_status columns to servers table
ALTER TABLE servers ADD COLUMN IF NOT EXISTS approval_status VARCHAR(50) NOT NULL DEFAULT 'PENDING';
ALTER TABLE servers ADD COLUMN IF NOT EXISTS approval_date TIMESTAMP WITH TIME ZONE;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS approval_reason TEXT;

-- Create index on approval_status column for servers
CREATE INDEX IF NOT EXISTS idx_servers_approval_status ON servers (approval_status);

-- Constraint: approval_status must be one of the valid values
ALTER TABLE servers ADD CONSTRAINT check_approval_status_valid
    CHECK (approval_status IN ('PENDING', 'APPROVED', 'DENIED'));

-- Add approval_status columns to agents table
ALTER TABLE agents ADD COLUMN IF NOT EXISTS approval_status VARCHAR(50) NOT NULL DEFAULT 'PENDING';
ALTER TABLE agents ADD COLUMN IF NOT EXISTS approval_date TIMESTAMP WITH TIME ZONE;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS approval_reason TEXT;

-- Create index on approval_status column for agents
CREATE INDEX IF NOT EXISTS idx_agents_approval_status ON agents (approval_status);

-- Constraint: approval_status must be one of the valid values
ALTER TABLE agents ADD CONSTRAINT check_approval_status_valid
    CHECK (approval_status IN ('PENDING', 'APPROVED', 'DENIED'));

-- Add approval_status columns to skills table
ALTER TABLE skills ADD COLUMN IF NOT EXISTS approval_status VARCHAR(50) NOT NULL DEFAULT 'PENDING';
ALTER TABLE skills ADD COLUMN IF NOT EXISTS approval_date TIMESTAMP WITH TIME ZONE;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS approval_reason TEXT;

-- Create index on approval_status column for skills
CREATE INDEX IF NOT EXISTS idx_skills_approval_status ON skills (approval_status);

-- Constraint: approval_status must be one of the valid values
ALTER TABLE skills ADD CONSTRAINT check_approval_status_valid
    CHECK (approval_status IN ('PENDING', 'APPROVED', 'DENIED'));

