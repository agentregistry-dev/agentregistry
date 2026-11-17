-- Remove the untouched Local runtime seeded by the v0.4.0 baseline. Local
-- runtimes created or modified by users are retained as inert records.
DELETE FROM runtimes
WHERE namespace = 'default'
  AND name = 'local'
  AND generation = 1
  AND labels = '{}'::jsonb
  AND annotations = '{}'::jsonb
  AND spec = '{"type":"Local"}'::jsonb
  AND status = '{}'::jsonb
  AND deletion_timestamp IS NULL
  AND finalizers = '[]'::jsonb;
