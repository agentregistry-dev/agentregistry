-- Rename enrichment findings resource identity from version to tag.
-- Existing dev/test databases may have applied migration 002 before the
-- public contract moved fully to metadata.tag. Fresh databases already get the
-- tag column from the edited 002 migration; this migration is a no-op there.

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'v1alpha1'
          AND table_name = 'enrichment_findings'
          AND column_name = 'version'
    ) AND NOT EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'v1alpha1'
          AND table_name = 'enrichment_findings'
          AND column_name = 'tag'
    ) THEN
        ALTER TABLE v1alpha1.enrichment_findings
            RENAME COLUMN version TO tag;
    END IF;
END $$;

DROP INDEX IF EXISTS v1alpha1.enrichment_findings_obj;
CREATE INDEX IF NOT EXISTS enrichment_findings_obj
    ON v1alpha1.enrichment_findings (kind, namespace, name, tag);
