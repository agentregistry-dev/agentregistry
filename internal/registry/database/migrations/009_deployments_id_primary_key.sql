-- Make deployment ID the primary key to allow multiple deployments
-- for the same server/version/resource tuple.

UPDATE deployments
SET id = uuid_generate_v4()::text
WHERE id IS NULL OR id = '';

ALTER TABLE deployments
    ALTER COLUMN id SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_id ON deployments (id);

ALTER TABLE deployments
    DROP CONSTRAINT IF EXISTS deployments_pkey;

-- Defensive re-create in case deployments_pkey previously owned the index.
CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_id ON deployments (id);

ALTER TABLE deployments
    ADD CONSTRAINT deployments_pkey PRIMARY KEY USING INDEX idx_deployments_id;

CREATE INDEX IF NOT EXISTS idx_deployments_identity_lookup
    ON deployments (resource_type, server_name, version, deployed_at DESC);
