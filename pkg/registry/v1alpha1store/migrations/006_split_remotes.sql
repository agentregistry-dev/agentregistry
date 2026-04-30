-- 006_split_remotes.sql
--
-- One-shot data migration: split MCPServer.spec.remotes into
-- RemoteMCPServer rows; rewrite affected Deployment.spec.targetRef.kind for
-- MCPServers that became remote-only (i.e. were deleted because they had no
-- packages). Agent.spec.remotes is unsupported and is stripped if present.
--
-- Idempotency is provided by the migration runner (schema_migrations
-- row gate); the SQL itself is also tolerant of replay because it
-- conditions every action on the source row still carrying a
-- non-empty `remotes` field.

-- -----------------------------------------------------------------------------
-- MCPServer split
-- -----------------------------------------------------------------------------

-- Insert one RemoteMCPServer per remote per source row.
-- Naming:
--   * single remote, no packages -> reuse source name (drop source row below)
--   * single remote, has packages -> source name + "-remote"
--   * multiple remotes -> source name + "-remote-<idx>"
-- The related-mcpserver annotation links siblings for the catalog UI.
INSERT INTO v1alpha1.remote_mcp_servers (
    namespace, name, version, generation, labels, annotations,
    spec, status, is_latest_version, created_at, updated_at
)
SELECT
    m.namespace,
    CASE
        WHEN jsonb_array_length(COALESCE(m.spec->'packages', '[]'::jsonb)) = 0
             AND jsonb_array_length(m.spec->'remotes') = 1
            THEN m.name
        WHEN jsonb_array_length(m.spec->'remotes') = 1
            THEN m.name || '-remote'
        ELSE m.name || '-remote-' || (r.ord - 1)::text
    END AS name,
    m.version,
    m.generation,
    m.labels,
    COALESCE(m.annotations, '{}'::jsonb)
        || jsonb_build_object('agentregistry.dev/related-mcpserver', m.name),
    jsonb_strip_nulls(jsonb_build_object(
        'title',       m.spec->'title',
        'description', m.spec->'description',
        'remote',      r.value
    )),
    '{}'::jsonb,
    -- Latest only when the source row was latest AND this is the first remote.
    (m.is_latest_version AND r.ord = 1) AS is_latest_version,
    m.created_at,
    m.updated_at
FROM v1alpha1.mcp_servers m,
     LATERAL jsonb_array_elements(COALESCE(m.spec->'remotes', '[]'::jsonb))
         WITH ORDINALITY AS r(value, ord)
WHERE jsonb_array_length(COALESCE(m.spec->'remotes', '[]'::jsonb)) > 0
ON CONFLICT (namespace, name, version) DO NOTHING;

-- Rewrite Deployment.targetRef.kind for deployments pointing at MCPServers
-- that are about to be deleted (had remotes and no packages). Must run
-- BEFORE the source rows are deleted so the JOIN can find them.
UPDATE v1alpha1.deployments d
SET spec = jsonb_set(
    d.spec,
    '{targetRef,kind}',
    to_jsonb('RemoteMCPServer'::text)
)
FROM v1alpha1.mcp_servers m
WHERE d.spec->'targetRef'->>'kind' = 'MCPServer'
  AND d.spec->'targetRef'->>'namespace' = m.namespace
  AND d.spec->'targetRef'->>'name'      = m.name
  AND COALESCE(d.spec->'targetRef'->>'version', '') IN ('', m.version)
  AND jsonb_array_length(COALESCE(m.spec->'remotes',  '[]'::jsonb)) > 0
  AND jsonb_array_length(COALESCE(m.spec->'packages', '[]'::jsonb)) = 0;

-- Delete MCPServer rows that became empty (had remotes, had no packages).
DELETE FROM v1alpha1.mcp_servers
WHERE jsonb_array_length(COALESCE(spec->'remotes', '[]'::jsonb)) > 0
  AND jsonb_array_length(COALESCE(spec->'packages', '[]'::jsonb)) = 0;

-- Strip remotes from MCPServer rows that survived (had packages).
UPDATE v1alpha1.mcp_servers
SET spec = spec - 'remotes'
WHERE spec ? 'remotes';

UPDATE v1alpha1.agents
SET spec = spec - 'remotes'
WHERE spec ? 'remotes';

-- Drop the obsolete preferRemote knob from any pre-existing deployment rows.
UPDATE v1alpha1.deployments
SET spec = spec - 'preferRemote'
WHERE spec ? 'preferRemote';
