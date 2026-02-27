-- Break glass migration: drop platform-specific deployment fields and
-- move to provider-agnostic metadata/config JSON objects.
-- No backfill is performed by design.

ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS provider_config JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS provider_metadata JSONB NOT NULL DEFAULT '{}'::jsonb;

DROP INDEX IF EXISTS idx_deployments_provider_resource;
DROP INDEX IF EXISTS idx_deployments_cloud_identity;
DROP INDEX IF EXISTS idx_deployments_cloud_resource_id;

ALTER TABLE deployments
    DROP COLUMN IF EXISTS region,
    DROP COLUMN IF EXISTS cloud_resource_id,
    DROP COLUMN IF EXISTS cloud_metadata;
