-- Introduce first-class providers and make deployments reference providers by ID.

CREATE TABLE IF NOT EXISTS providers (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    platform VARCHAR(50) NOT NULL,
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT check_provider_platform_valid CHECK (platform IN ('local', 'kubernetes', 'gcp', 'aws'))
);

INSERT INTO providers (id, name, platform, config)
VALUES
    ('local', 'Local', 'local', '{}'::jsonb),
    ('kubernetes-default', 'Kubernetes Default', 'kubernetes', '{}'::jsonb)
ON CONFLICT (id) DO NOTHING;

ALTER TABLE deployments ADD COLUMN IF NOT EXISTS provider_id TEXT;

UPDATE deployments
SET provider_id = NULLIF(connection_id, '')
WHERE provider_id IS NULL AND connection_id IS NOT NULL;

INSERT INTO providers (id, name, platform, config)
SELECT DISTINCT
    d.provider_id,
    d.provider_id,
    COALESCE(NULLIF(TRIM(d.provider), ''), 'local'),
    '{}'::jsonb
FROM deployments d
WHERE d.provider_id IS NOT NULL
  AND d.provider_id <> ''
ON CONFLICT (id) DO NOTHING;

UPDATE deployments
SET provider_id = 'local'
WHERE provider_id IS NULL OR provider_id = '';

ALTER TABLE deployments
    ALTER COLUMN provider_id SET DEFAULT 'local',
    ALTER COLUMN provider_id SET NOT NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_deployments_provider_id'
    ) THEN
        ALTER TABLE deployments
            ADD CONSTRAINT fk_deployments_provider_id
            FOREIGN KEY (provider_id) REFERENCES providers(id) ON DELETE RESTRICT;
    END IF;
END $$;

DROP INDEX IF EXISTS idx_deployments_connection_id;
DROP INDEX IF EXISTS idx_deployments_connection_resource;
CREATE INDEX IF NOT EXISTS idx_deployments_provider_id ON deployments (provider_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_deployments_provider_resource
    ON deployments (provider_id, cloud_resource_id)
    WHERE provider_id IS NOT NULL AND cloud_resource_id IS NOT NULL;

ALTER TABLE deployments DROP COLUMN IF EXISTS connection_id;
ALTER TABLE deployments DROP COLUMN IF EXISTS provider;
