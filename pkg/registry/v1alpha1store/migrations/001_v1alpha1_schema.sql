-- v1alpha1 schema: every resource uses the same envelope (apiVersion +
-- metadata + spec + status). Metadata fields are promoted to real columns;
-- spec and status stay JSONB. Content resources use (namespace, name, tag);
-- Provider and Deployment use mutable-object storage (namespace/name).
--
-- Tagged-artifact tables (agents, mcp_servers, remote_mcp_servers, skills,
-- prompts) store one mutable row per tag. The store fills omitted tags with the
-- literal "latest" tag, and same-tag applies replace the prior row atomically.
-- "Latest" is a normal tag value, not the most recently applied live tag.
--
-- Providers and Deployments are not tagged-artifact rows — they're
-- infra/lifecycle state keyed directly by namespace/name.
--
-- All tables live under the dedicated PostgreSQL schema `v1alpha1` so they
-- coexist with the older `public.agents`, `public.servers`, etc. during
-- the incremental port. Callers using the new generic Store pass
-- schema-qualified table names (e.g. "v1alpha1.agents"); older
-- postgres_*.go stores continue to read/write the unqualified public tables
-- without conflict. Final cutover drops the old tables and either keeps
-- the v1alpha1 schema or renames it to public.
--
-- Authoritative schema for spec + status JSONB is the Go type system under
-- pkg/api/v1alpha1 (Agent/MCPServer/Skill/Prompt/Runtime/Deployment typed
-- envelopes). Validation is enforced at the API boundary by
-- (*Kind).Validate(); this layer does NOT add JSON schema CHECK constraints.

CREATE SCHEMA IF NOT EXISTS v1alpha1;

-- -----------------------------------------------------------------------------
-- Shared helpers (schema-qualified so they don't collide with older triggers)
-- -----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION v1alpha1.set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- notify_status_change fires a pg_notify on the table's notification channel
-- only when the status column changes. Spec/metadata writes do not notify —
-- reconcilers subscribe to status-only events so they observe reconciliation
-- convergence without being woken up by idempotent re-applies.
--
-- Payload: {"op": "INSERT|UPDATE|DELETE", "id": "<namespace>/<name>[/<tag>]"}
CREATE OR REPLACE FUNCTION v1alpha1.notify_status_change()
RETURNS TRIGGER AS $$
DECLARE
    channel TEXT := TG_ARGV[0];
    payload JSON;
    op TEXT;
BEGIN
    IF TG_OP = 'INSERT' THEN
        op := 'INSERT';
    ELSIF TG_OP = 'DELETE' THEN
        op := 'DELETE';
        payload := json_build_object(
            'op', op,
            'id', OLD.namespace || '/' || OLD.name ||
                COALESCE('/' || (to_jsonb(OLD)->>'tag'), ''));
        PERFORM pg_notify(channel, payload::text);
        RETURN OLD;
    ELSE
        op := 'UPDATE';
        IF NEW.status::text = OLD.status::text THEN
            RETURN NEW;
        END IF;
    END IF;
    payload := json_build_object(
        'op', op,
        'id', NEW.namespace || '/' || NEW.name ||
            COALESCE('/' || (to_jsonb(NEW)->>'tag'), ''));
    PERFORM pg_notify(channel, payload::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Tagged-artifact tables: identical shape across agents, mcp_servers,
-- skills, prompts. (remote_mcp_servers shares the shape and is created
-- in 005_remote_resources.sql.)
--
-- Columns:
--   namespace, name, tag        — composite identity (PK).
--   generation                 — server-managed convergence counter
--   labels, annotations        — user-set key/value JSONB
--   spec                       — JSONB per pkg/api/v1alpha1 typed Spec
--   content_hash               — SHA-256 hex of spec plus relevant metadata;
--                                Upsert short-circuits when the incoming
--                                hash matches the existing tag row's hash.
--   status                     — JSONB per v1alpha1.Status.
--   deletion_timestamp         — server-managed soft-delete marker
--   created_at, updated_at     — timestamps (trigger-maintained)
--
-- The `uid` column carries a DEFAULT of gen_random_uuid() so it is the sole
-- UID issuer: the Go store omits it from the INSERT column list on Upsert
-- (the default fires on create) and keeps it out of the ON CONFLICT DO UPDATE
-- SET list (so it is preserved across re-applies — Kubernetes-style read-only
-- metadata.uid). Direct-SQL inserts (the runtimes seed below, manual psql)
-- get a valid UID without code changes.
--
-- Indexes:
--   PK (namespace, name, tag) supports per-tag lookups.
--   (namespace, name, updated_at DESC, tag) serves "list tags by newest apply"
--   queries.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS v1alpha1.agents (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    tag                VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    content_hash       CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, tag)
);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_name_tag_updated_desc ON v1alpha1.agents (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_labels_gin        ON v1alpha1.agents USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_spec_gin          ON v1alpha1.agents USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_updated_at_desc   ON v1alpha1.agents (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_terminating       ON v1alpha1.agents (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;

CREATE OR REPLACE TRIGGER agents_set_updated_at  BEFORE UPDATE ON v1alpha1.agents  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER agents_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.agents  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_agents_status');

CREATE TABLE IF NOT EXISTS v1alpha1.mcp_servers (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    tag                VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    content_hash       CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, tag)
);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_name_tag_updated_desc ON v1alpha1.mcp_servers (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_labels_gin        ON v1alpha1.mcp_servers USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_spec_gin          ON v1alpha1.mcp_servers USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_updated_at_desc   ON v1alpha1.mcp_servers (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_terminating       ON v1alpha1.mcp_servers (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER mcp_servers_set_updated_at  BEFORE UPDATE ON v1alpha1.mcp_servers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER mcp_servers_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.mcp_servers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_mcp_servers_status');

CREATE TABLE IF NOT EXISTS v1alpha1.skills (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    tag                VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    content_hash       CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, tag)
);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_name_tag_updated_desc ON v1alpha1.skills (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_labels_gin        ON v1alpha1.skills USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_spec_gin          ON v1alpha1.skills USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_updated_at_desc   ON v1alpha1.skills (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_terminating       ON v1alpha1.skills (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER skills_set_updated_at  BEFORE UPDATE ON v1alpha1.skills  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER skills_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.skills  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_skills_status');

CREATE TABLE IF NOT EXISTS v1alpha1.prompts (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    tag                VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    content_hash       CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, tag)
);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_name_tag_updated_desc ON v1alpha1.prompts (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_labels_gin        ON v1alpha1.prompts USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_spec_gin          ON v1alpha1.prompts USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_updated_at_desc   ON v1alpha1.prompts (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_terminating       ON v1alpha1.prompts (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER prompts_set_updated_at  BEFORE UPDATE ON v1alpha1.prompts  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER prompts_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.prompts  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_prompts_status');

-- -----------------------------------------------------------------------------
-- Runtimes and Deployments: lifecycle/infra state, NOT tagged artifacts.
-- Both use Kubernetes-like mutable storage. Runtime belongs with Deployment as
-- infra/config — the actual tagged artifacts that get deployed are
-- Agents/MCPServers/Skills/Prompts/RemoteMCPServers.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS v1alpha1.runtimes (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    deletion_timestamp TIMESTAMPTZ,
    finalizers         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name)
);
CREATE INDEX IF NOT EXISTS v1alpha1_runtimes_labels_gin      ON v1alpha1.runtimes USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_runtimes_spec_gin        ON v1alpha1.runtimes USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_runtimes_updated_at_desc ON v1alpha1.runtimes (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_runtimes_terminating    ON v1alpha1.runtimes (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER runtimes_set_updated_at  BEFORE UPDATE ON v1alpha1.runtimes  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER runtimes_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.runtimes  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_runtimes_status');

CREATE TABLE IF NOT EXISTS v1alpha1.deployments (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    deletion_timestamp TIMESTAMPTZ,
    finalizers         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name)
);
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_labels_gin      ON v1alpha1.deployments USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_spec_gin        ON v1alpha1.deployments USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_updated_at_desc ON v1alpha1.deployments (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_terminating    ON v1alpha1.deployments (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER deployments_set_updated_at  BEFORE UPDATE ON v1alpha1.deployments  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER deployments_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.deployments  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_deployments_status');

-- -----------------------------------------------------------------------------
-- Seed: default runtimes so deployments can reference them out-of-the-box.
-- Seeded in the `default` namespace.
-- -----------------------------------------------------------------------------

INSERT INTO v1alpha1.runtimes (namespace, name, spec)
VALUES
    ('default', 'local',              '{"type":"Local"}'::jsonb),
    ('default', 'kubernetes-default', '{"type":"Kubernetes"}'::jsonb)
ON CONFLICT (namespace, name) DO NOTHING;
