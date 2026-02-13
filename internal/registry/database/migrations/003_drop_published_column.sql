-- Drop the published boolean column from all resource tables.
-- The publish/unpublish workflow has been removed: all resources are
-- immediately visible on creation. The published_at timestamp is
-- preserved (it's part of the upstream MCP registry spec and used for
-- version ordering).

-- Also drop the unpublished_date and published_date columns which are
-- artifacts of the old workflow. published_at serves as the canonical
-- timestamp.

ALTER TABLE servers DROP COLUMN IF EXISTS published;
ALTER TABLE servers DROP COLUMN IF EXISTS published_date;
ALTER TABLE servers DROP COLUMN IF EXISTS unpublished_date;
DROP INDEX IF EXISTS idx_servers_published;

ALTER TABLE agents DROP COLUMN IF EXISTS published;
ALTER TABLE agents DROP COLUMN IF EXISTS published_date;
ALTER TABLE agents DROP COLUMN IF EXISTS unpublished_date;
DROP INDEX IF EXISTS idx_agents_published;

ALTER TABLE skills DROP COLUMN IF EXISTS published;
ALTER TABLE skills DROP COLUMN IF EXISTS published_date;
ALTER TABLE skills DROP COLUMN IF EXISTS unpublished_date;
DROP INDEX IF EXISTS idx_skills_published;
