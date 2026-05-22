-- Tighten MCPServer.metadata.name to DNS-1123 label (matches the application
-- validator in pkg/api/v1alpha1: ^[a-z0-9]([-a-z0-9]*[a-z0-9])?$, max 63 chars).
--
-- Non-compliant rows are auto-rewritten. The original name is preserved in:
--   - spec.source.package.mcpName  (for rows that have a Package AND whose
--                                    original name matches the upstream
--                                    catalogue pattern `namespace/name`, so
--                                    the ownership validator keeps matching
--                                    the upstream package's published name.
--                                    Originals that don't match the upstream
--                                    pattern would fail validateMCPPackageName
--                                    on the next write, so we skip mcpName
--                                    preservation for those and emit a
--                                    NOTICE listing them — the annotation
--                                    below is the durable record either way.)
--   - metadata.annotations["agentregistry.dev/migrated-from-name"]
--                                   (durable audit trail for every rewritten
--                                    row, including Remote / Repository-only)
--
-- Cascaded into:
--   - Deployment.spec.targetRef.name (where kind = MCPServer)
--   - Agent.spec.mcpServers[].name   (any element with kind = MCPServer)
--
-- generation is bumped on every mutated row so observers tracking change via
-- generation pick up the rewrite.
--
-- A summary NOTICE is emitted per rewrite group so operators see the renames
-- in the server's log stream (postgres OnNotice → slog wiring in
-- internal/registry/database/postgres.go).
--
-- Idempotent: WHERE clauses skip already-compliant rows.
--
-- Aborts cleanly when any sanitized name would collide with another row in
-- the same namespace — whether the colliding partner is itself non-compliant
-- or already compliant. Operator resolves manually (rename one side) and
-- re-runs the binary.
--
-- Atomicity: the migrator (pkg/registry/database/migrate.go) wraps each
-- migration file in a single transaction, so the pre-flights, the four
-- UPDATEs, and the DROP FUNCTION below either all apply or none do. If you
-- edit this file, preserve that assumption — splitting work across
-- transactions would let a mid-file failure leave mcp_servers renamed but
-- the cascades un-applied.
--
-- Dangling refs: the cascade rewrites Deployment.targetRef and
-- Agent.mcpServers[] entries by name shape, not by joining to mcp_servers.
-- A ref that was already dangling (pointing at no real MCPServer) stays
-- dangling, but its name string is sanitized. This is intentional —
-- sanitization is deterministic, so a ref's resolution outcome is
-- preserved across the rename.

-- Session-scoped sanitization helper. lower() runs BEFORE regexp_replace so
-- uppercase letters survive as their lowercase equivalents (the character
-- class [a-z0-9-] is case-sensitive in POSIX regex — applying lower() after
-- the replace would have eaten every uppercase letter). Trim hyphens after
-- the substring so runs of disallowed chars collapse without leaving stray
-- leading/trailing -.
CREATE OR REPLACE FUNCTION pg_temp.sanitize_mcp_name(s TEXT) RETURNS TEXT AS $$
    SELECT NULLIF(trim(both '-' from substring(
        regexp_replace(lower(s), '[^a-z0-9-]+', '-', 'g'),
        1, 63
    )), '');
$$ LANGUAGE SQL IMMUTABLE;

DO $$
DECLARE
    collisions TEXT;
    rewrite_count INT;
    rewrite_log TEXT;
    skipped_mcpname_count INT;
    skipped_mcpname_log TEXT;
BEGIN
    -- Pre-flight 1: collision detection across ALL rows (both non-compliant
    -- and already-compliant). Catches two non-compliant originals sanitizing
    -- to the same form AND a non-compliant original sanitizing to an
    -- already-existing compliant name.
    WITH all_names AS (
        SELECT
            namespace,
            name AS original_name,
            CASE WHEN name !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' OR length(name) > 63
                 THEN pg_temp.sanitize_mcp_name(name)
                 ELSE name
            END AS final_name
        FROM v1alpha1.mcp_servers
    )
    SELECT string_agg(
        format('%s/%s <- %s', namespace, final_name, array_to_string(originals, ', ')),
        E'\n'
    )
    INTO collisions
    FROM (
        SELECT namespace, final_name, array_agg(DISTINCT original_name) AS originals
        FROM all_names
        WHERE final_name IS NOT NULL
        GROUP BY namespace, final_name
        HAVING COUNT(DISTINCT original_name) > 1
    ) c;

    IF collisions IS NOT NULL THEN
        RAISE EXCEPTION
            E'MCP DNS-label migration: collisions detected; manually rename one side and retry.\n%',
            collisions;
    END IF;

    -- Pre-flight 2: reject rows whose sanitized form is empty.
    IF EXISTS (
        SELECT 1 FROM v1alpha1.mcp_servers
         WHERE (name !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' OR length(name) > 63)
           AND pg_temp.sanitize_mcp_name(name) IS NULL
    ) THEN
        RAISE EXCEPTION
            'MCP DNS-label migration: one or more MCPServer names contain no DNS-label-compatible characters; rename manually and retry.';
    END IF;

    -- Emit a per-rewrite summary so operators see what the migration did.
    SELECT count(*),
           string_agg(format('  %s/%s -> %s/%s', namespace, name, namespace, pg_temp.sanitize_mcp_name(name)), E'\n')
      INTO rewrite_count, rewrite_log
      FROM v1alpha1.mcp_servers
     WHERE name !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' OR length(name) > 63;
    IF rewrite_count > 0 THEN
        RAISE NOTICE E'MCP DNS-label migration: rewriting % MCPServer row(s):\n%', rewrite_count, rewrite_log;
    END IF;

    -- Flag package-bearing rows whose original name doesn't match the
    -- upstream `namespace/name` pattern. The mcpName-preservation UPDATE
    -- below skips these (it would fail validateMCPPackageName on next
    -- write); the audit annotation still captures the original name.
    SELECT count(*),
           string_agg(format('  %s/%s', namespace, name), E'\n')
      INTO skipped_mcpname_count, skipped_mcpname_log
      FROM v1alpha1.mcp_servers
     WHERE (name !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' OR length(name) > 63)
       AND spec->'source'->'package' IS NOT NULL
       AND NOT (spec->'source'->'package' ? 'mcpName')
       AND (name !~ '^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$'
            OR length(name) NOT BETWEEN 3 AND 200);
    IF skipped_mcpname_count > 0 THEN
        RAISE NOTICE E'MCP DNS-label migration: % package-bearing row(s) had non-upstream-shaped names; spec.source.package.mcpName left unset (annotation retains the original):\n%',
            skipped_mcpname_count, skipped_mcpname_log;
    END IF;
END $$;

-- Preserve original name in spec.source.package.mcpName for non-compliant
-- rows that carry a Package, haven't already set mcpName, AND whose original
-- matches the upstream `namespace/name` catalogue pattern (3-200 chars).
-- Originals outside that shape would fail validateMCPPackageName on the next
-- write — those rows are skipped here and reported as a NOTICE in the DO
-- block above; the annotation below still records the original name.
UPDATE v1alpha1.mcp_servers
   SET spec = jsonb_set(spec, '{source,package,mcpName}', to_jsonb(name), true)
 WHERE (name !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$' OR length(name) > 63)
   AND spec->'source'->'package' IS NOT NULL
   AND NOT (spec->'source'->'package' ? 'mcpName')
   AND name ~ '^[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+$'
   AND length(name) BETWEEN 3 AND 200;

-- Annotate every rewritten row with the original name for durable audit.
-- Bump generation since spec+annotations changed.
UPDATE v1alpha1.mcp_servers
   SET annotations = jsonb_set(annotations, '{agentregistry.dev/migrated-from-name}', to_jsonb(name), true),
       name = pg_temp.sanitize_mcp_name(name),
       generation = generation + 1
 WHERE name !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
    OR length(name) > 63;

-- Cascade: Deployment.spec.targetRef.name pointing at a renamed MCPServer.
UPDATE v1alpha1.deployments
   SET spec = jsonb_set(spec, '{targetRef,name}',
        to_jsonb(pg_temp.sanitize_mcp_name(spec->'targetRef'->>'name'))),
       generation = generation + 1
 WHERE spec->'targetRef'->>'kind' = 'MCPServer'
   AND (spec->'targetRef'->>'name' !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
        OR length(spec->'targetRef'->>'name') > 63);

-- Cascade: Agent.spec.mcpServers[].name entries that point at a renamed
-- MCPServer. Rebuild the array with the sanitized name swapped in for
-- matching elements only. WITH ORDINALITY + ORDER BY ord pins the rebuilt
-- array's element order to the source array — jsonb_agg without ORDER BY
-- preserves input order in practice on PG16 but isn't formally guaranteed.
UPDATE v1alpha1.agents
   SET spec = jsonb_set(
       spec,
       '{mcpServers}',
       (SELECT jsonb_agg(
           CASE WHEN elem->>'kind' = 'MCPServer'
                AND (elem->>'name' !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
                     OR length(elem->>'name') > 63)
                THEN jsonb_set(elem, '{name}', to_jsonb(pg_temp.sanitize_mcp_name(elem->>'name')))
                ELSE elem
           END ORDER BY ord)
       FROM jsonb_array_elements(spec->'mcpServers') WITH ORDINALITY AS t(elem, ord))
   ),
   generation = generation + 1
 WHERE jsonb_typeof(spec->'mcpServers') = 'array'
   AND EXISTS (
       SELECT 1 FROM jsonb_array_elements(spec->'mcpServers') AS elem
       WHERE elem->>'kind' = 'MCPServer'
         AND (elem->>'name' !~ '^[a-z0-9]([-a-z0-9]*[a-z0-9])?$'
              OR length(elem->>'name') > 63)
   );

-- No DROP FUNCTION needed: pg_temp objects die with the session.
