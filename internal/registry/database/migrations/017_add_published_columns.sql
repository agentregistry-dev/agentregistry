-- Add published columns to servers, agents, and skills tables

-- Add published columns to servers table
ALTER TABLE servers ADD COLUMN IF NOT EXISTS published BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS published_date TIMESTAMP WITH TIME ZONE;
ALTER TABLE servers ADD COLUMN IF NOT EXISTS unpublished_date TIMESTAMP WITH TIME ZONE;

-- Create index on published column for servers
CREATE INDEX IF NOT EXISTS idx_servers_published ON servers (published);

-- Add published columns to agents table
ALTER TABLE agents ADD COLUMN IF NOT EXISTS published BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS published_date TIMESTAMP WITH TIME ZONE;
ALTER TABLE agents ADD COLUMN IF NOT EXISTS unpublished_date TIMESTAMP WITH TIME ZONE;

-- Create index on published column for agents
CREATE INDEX IF NOT EXISTS idx_agents_published ON agents (published);

-- Add published columns to skills table
ALTER TABLE skills ADD COLUMN IF NOT EXISTS published BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS published_date TIMESTAMP WITH TIME ZONE;
ALTER TABLE skills ADD COLUMN IF NOT EXISTS unpublished_date TIMESTAMP WITH TIME ZONE;

-- Create index on published column for skills
CREATE INDEX IF NOT EXISTS idx_skills_published ON skills (published);

