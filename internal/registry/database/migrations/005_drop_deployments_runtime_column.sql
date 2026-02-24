-- Remove legacy runtime column now that provider is the only deployment discriminator.

DROP INDEX IF EXISTS idx_deployments_runtime;
ALTER TABLE deployments DROP CONSTRAINT IF EXISTS check_deployment_runtime_valid;
ALTER TABLE deployments DROP COLUMN IF EXISTS runtime;
