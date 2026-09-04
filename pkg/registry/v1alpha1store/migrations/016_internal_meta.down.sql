ALTER TABLE deployments
    DROP COLUMN IF EXISTS internal_meta;

ALTER TABLE runtimes
    DROP COLUMN IF EXISTS internal_meta;
