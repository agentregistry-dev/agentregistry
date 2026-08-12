-- Remove the untouched Kubernetes runtime seeded by the v0.4.0 baseline when
-- no Deployment references it. User-created, modified, or referenced runtime
-- rows are retained as inert records for external integrations to migrate.
DELETE FROM runtimes AS runtime
WHERE runtime.namespace = 'default'
  AND runtime.name = 'kubernetes-default'
  AND runtime.generation = 1
  AND runtime.labels = '{}'::jsonb
  AND runtime.annotations = '{}'::jsonb
  AND runtime.spec = '{"type":"Kubernetes"}'::jsonb
  AND runtime.status = '{}'::jsonb
  AND runtime.deletion_timestamp IS NULL
  AND runtime.finalizers = '[]'::jsonb
  AND NOT EXISTS (
    SELECT 1
    FROM deployments AS deployment
    WHERE deployment.spec->'runtimeRef'->>'name' = runtime.name
      AND COALESCE(
        NULLIF(deployment.spec->'runtimeRef'->>'namespace', ''),
        deployment.namespace
      ) = runtime.namespace
  );
