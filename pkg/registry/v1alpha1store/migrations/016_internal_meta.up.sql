ALTER TABLE runtimes
    ADD COLUMN IF NOT EXISTS internal_meta JSONB NOT NULL DEFAULT '{}'::jsonb;

ALTER TABLE deployments
    ADD COLUMN IF NOT EXISTS internal_meta JSONB NOT NULL DEFAULT '{}'::jsonb;
