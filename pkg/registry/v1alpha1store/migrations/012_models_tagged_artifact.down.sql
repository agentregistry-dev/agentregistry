-- A tagged Model table can be collapsed back to mutable identity only while
-- each namespace/name has at most one tag. Refuse a lossy rollback.
DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM models
        GROUP BY namespace, name
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot roll back tagged Models with multiple tags per namespace/name';
    END IF;
END;
$$;

DROP INDEX IF EXISTS models_tags_alive;
DROP INDEX IF EXISTS models_list_alive;

ALTER TABLE models
    ADD COLUMN IF NOT EXISTS finalizers jsonb DEFAULT '[]'::jsonb NOT NULL;

ALTER TABLE models
    DROP CONSTRAINT IF EXISTS models_pkey;

ALTER TABLE models
    DROP COLUMN IF EXISTS content_hash;

ALTER TABLE models
    DROP COLUMN IF EXISTS tag;

ALTER TABLE models
    ADD PRIMARY KEY (namespace, name);
