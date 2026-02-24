-- Evolve OSS deployments toward unified provider-based model.
-- Keeps existing columns for compatibility while introducing stable IDs
-- and provider/origin metadata needed by enterprise extension points.

ALTER TABLE deployments ADD COLUMN IF NOT EXISTS id TEXT;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS provider VARCHAR(50);
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS origin VARCHAR(50) NOT NULL DEFAULT 'managed';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS connection_id TEXT;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS region TEXT;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS cloud_resource_id TEXT;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS cloud_metadata JSONB DEFAULT '{}'::jsonb;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS deployed_by TEXT DEFAULT '';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS error TEXT DEFAULT '';

-- Backfill IDs and provider for existing rows.
UPDATE deployments SET id = COALESCE(id, uuid_generate_v4()::text) WHERE id IS NULL OR id = '';
UPDATE deployments SET provider = 'local' WHERE provider = 'docker';
UPDATE deployments SET provider = COALESCE(NULLIF(provider, ''), 'local')
WHERE provider IS NULL OR provider = '';

-- Ensure ID exists and is unique.
ALTER TABLE deployments ALTER COLUMN id SET NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_id ON deployments (id);

-- Enforce provider/origin values.
ALTER TABLE deployments ALTER COLUMN provider SET NOT NULL;
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS check_deployment_provider_valid;
ALTER TABLE deployments ADD CONSTRAINT check_deployment_provider_valid
    CHECK (provider IN ('local', 'kubernetes', 'gcp', 'aws'));
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS check_deployment_origin_valid;
ALTER TABLE deployments ADD CONSTRAINT check_deployment_origin_valid
    CHECK (origin IN ('managed', 'discovered'));

-- Extend status vocabulary for unified async/cloud semantics.
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS check_deployment_status_valid;
ALTER TABLE deployments ADD CONSTRAINT check_deployment_status_valid
    CHECK (status IN ('active', 'deploying', 'deployed', 'failed', 'cancelled', 'discovered', 'stopped'));

-- New indexes for provider/discovery queries.
CREATE INDEX IF NOT EXISTS idx_deployments_provider ON deployments (provider);
CREATE INDEX IF NOT EXISTS idx_deployments_origin ON deployments (origin);
CREATE INDEX IF NOT EXISTS idx_deployments_connection_id ON deployments (connection_id);
CREATE INDEX IF NOT EXISTS idx_deployments_cloud_resource_id ON deployments (cloud_resource_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_cloud_identity
    ON deployments (connection_id, cloud_resource_id)
    WHERE cloud_resource_id IS NOT NULL AND cloud_resource_id <> '';

