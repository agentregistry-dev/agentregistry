-- Convert Models from mutable namespace/name objects to tagged catalog
-- artifacts. Existing rows become the literal "latest" tag so references
-- that omit tag preserve their pre-migration behavior.

ALTER TABLE models
    ADD COLUMN IF NOT EXISTS tag character varying(255);

UPDATE models
SET tag = 'latest'
WHERE tag IS NULL;

ALTER TABLE models
    ALTER COLUMN tag SET NOT NULL;

-- Tagged stores use content_hash for same-tag replacement detection. The
-- sentinel deliberately forces the first post-migration re-apply to recompute
-- the canonical application hash without changing the migrated row's data.
ALTER TABLE models
    ADD COLUMN IF NOT EXISTS content_hash character(64);

UPDATE models
SET content_hash = repeat('0', 64)
WHERE content_hash IS NULL;

ALTER TABLE models
    ALTER COLUMN content_hash SET NOT NULL;

ALTER TABLE models
    DROP CONSTRAINT IF EXISTS models_pkey;

ALTER TABLE models
    ADD PRIMARY KEY (namespace, name, tag);

-- Finalizers are mutable-object lifecycle state. Tagged artifacts are deleted
-- by exact tag (or all tags through the batch endpoint) and do not use them.
ALTER TABLE models
    DROP COLUMN IF EXISTS finalizers;

CREATE INDEX IF NOT EXISTS models_list_alive
    ON models USING btree (namespace, name, tag, updated_at)
    WHERE deletion_timestamp IS NULL;

CREATE INDEX IF NOT EXISTS models_tags_alive
    ON models USING btree (namespace, name, updated_at DESC, tag DESC)
    WHERE deletion_timestamp IS NULL;
