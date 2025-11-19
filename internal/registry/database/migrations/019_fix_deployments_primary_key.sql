-- Fix deployments table primary key to allow multiple versions of the same server
-- Change from PRIMARY KEY (server_name) to PRIMARY KEY (server_name, version)
-- This allows deploying multiple versions of the same server simultaneously

-- Drop the existing primary key constraint
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS deployments_pkey;

-- Create new composite primary key using server_name + version
ALTER TABLE deployments ADD CONSTRAINT deployments_pkey PRIMARY KEY (server_name, version);

-- The idx_deployments_server_name index is still useful for queries filtering by server_name only
-- so we keep it

