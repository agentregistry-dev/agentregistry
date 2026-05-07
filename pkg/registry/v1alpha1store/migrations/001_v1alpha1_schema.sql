-- v1alpha1 schema: every resource uses the same envelope (apiVersion +
-- metadata + spec + status). Metadata fields are promoted to real columns;
-- spec and status stay JSONB. (namespace, name, version) is the composite
-- primary key for every kind.
--
-- Versioned-artifact tables (agents, mcp_servers, remote_mcp_servers,
-- skills, prompts) are append-only with system-assigned monotonic
-- INTEGER versions. Each row carries a SHA-256 spec_hash so Upsert can
-- recognise an unchanged spec and skip emitting a new version. "Latest"
-- is computed as MAX(version) over the live rows for a (namespace,
-- name); the per-table (namespace, name, version DESC) index serves
-- that lookup.
--
-- Providers and Deployments are not versioned-artifact rows — they're
-- infra/lifecycle state and keep the older string-version shape (out of
-- scope for the immutable-versioning redesign).
--
-- All tables live under the dedicated PostgreSQL schema `v1alpha1` so they
-- coexist with the legacy `public.agents`, `public.servers`, etc. during
-- the incremental port. Callers using the new generic Store pass
-- schema-qualified table names (e.g. "v1alpha1.agents"); legacy
-- postgres_*.go stores continue to read/write the unqualified public tables
-- without conflict. Final cutover drops the old tables and either keeps
-- the v1alpha1 schema or renames it to public.
--
-- Authoritative schema for spec + status JSONB is the Go type system under
-- pkg/api/v1alpha1 (Agent/MCPServer/Skill/Prompt/Provider/Deployment typed
-- envelopes). Validation is enforced at the API boundary by
-- (*Kind).Validate(); this layer does NOT add JSON schema CHECK constraints.

CREATE SCHEMA IF NOT EXISTS v1alpha1;

-- -----------------------------------------------------------------------------
-- Shared helpers (schema-qualified so they don't collide with legacy triggers)
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
-- Payload: {"op": "INSERT|UPDATE|DELETE", "id": "<namespace>/<name>/<version>"}
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
            'id', OLD.namespace || '/' || OLD.name || '/' || OLD.version);
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
        'id', NEW.namespace || '/' || NEW.name || '/' || NEW.version);
    PERFORM pg_notify(channel, payload::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- -----------------------------------------------------------------------------
-- Versioned-artifact tables: identical shape across agents, mcp_servers,
-- skills, prompts. (remote_mcp_servers shares the shape and is created
-- in 005_remote_resources.sql.)
--
-- Columns:
--   namespace, name, version   — composite identity (PK); version is a
--                                positive integer assigned by the store on
--                                Upsert (MAX(version)+1).
--   uid                        — server-assigned UUID, stamped at row creation
--                                and never mutated; DEFAULT gen_random_uuid()
--                                so direct-SQL inserts also get a valid UID.
--   labels, annotations        — user-set key/value JSONB
--   spec                       — JSONB per pkg/api/v1alpha1 typed Spec
--   spec_hash                  — SHA-256 hex of the canonical-JSON spec;
--                                Upsert short-circuits when the incoming
--                                hash matches the latest live row's hash.
--   status                     — JSONB per v1alpha1.Status (Status.Version
--                                mirrors the row's version column).
--   deletion_timestamp         — server-managed soft-delete marker
--   created_at, updated_at     — timestamps (trigger-maintained)
--
-- Indexes:
--   PK (namespace, name, version) supports per-version lookups.
--   (namespace, name, version DESC) serves "give me the latest live row"
--   queries (MAX(version) + ORDER BY version DESC LIMIT 1).
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS v1alpha1.agents (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    version            INTEGER      NOT NULL CHECK (version > 0),
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    spec_hash          CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, version)
);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_name_version_desc ON v1alpha1.agents (namespace, name, version DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_labels_gin        ON v1alpha1.agents USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_spec_gin          ON v1alpha1.agents USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_updated_at_desc   ON v1alpha1.agents (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_agents_terminating       ON v1alpha1.agents (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;

CREATE OR REPLACE TRIGGER agents_set_updated_at  BEFORE UPDATE ON v1alpha1.agents  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER agents_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.agents  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_agents_status');

CREATE TABLE IF NOT EXISTS v1alpha1.mcp_servers (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    version            INTEGER      NOT NULL CHECK (version > 0),
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    spec_hash          CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, version)
);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_name_version_desc ON v1alpha1.mcp_servers (namespace, name, version DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_labels_gin        ON v1alpha1.mcp_servers USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_spec_gin          ON v1alpha1.mcp_servers USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_updated_at_desc   ON v1alpha1.mcp_servers (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_mcp_servers_terminating       ON v1alpha1.mcp_servers (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER mcp_servers_set_updated_at  BEFORE UPDATE ON v1alpha1.mcp_servers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER mcp_servers_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.mcp_servers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_mcp_servers_status');

CREATE TABLE IF NOT EXISTS v1alpha1.skills (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    version            INTEGER      NOT NULL CHECK (version > 0),
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    spec_hash          CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, version)
);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_name_version_desc ON v1alpha1.skills (namespace, name, version DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_labels_gin        ON v1alpha1.skills USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_spec_gin          ON v1alpha1.skills USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_updated_at_desc   ON v1alpha1.skills (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_skills_terminating       ON v1alpha1.skills (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER skills_set_updated_at  BEFORE UPDATE ON v1alpha1.skills  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER skills_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.skills  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_skills_status');

CREATE TABLE IF NOT EXISTS v1alpha1.prompts (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    version            INTEGER      NOT NULL CHECK (version > 0),
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    spec_hash          CHAR(64)     NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deletion_timestamp TIMESTAMPTZ,
    PRIMARY KEY (namespace, name, version)
);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_name_version_desc ON v1alpha1.prompts (namespace, name, version DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_labels_gin        ON v1alpha1.prompts USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_spec_gin          ON v1alpha1.prompts USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_updated_at_desc   ON v1alpha1.prompts (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_prompts_terminating       ON v1alpha1.prompts (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER prompts_set_updated_at  BEFORE UPDATE ON v1alpha1.prompts  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER prompts_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.prompts  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_prompts_status');

-- -----------------------------------------------------------------------------
-- Providers and Deployments: lifecycle/infra state, NOT versioned artifacts.
-- Both retain the pre-immutable-versioning shape (string version, generation,
-- finalizers, is_latest_version). Provider belongs with Deployment as
-- infra/config — the actual versioned artifacts that get deployed are
-- Agents/MCPServers/Skills/Prompts/RemoteMCPServers.
-- -----------------------------------------------------------------------------

CREATE TABLE IF NOT EXISTS v1alpha1.providers (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    version            VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version  BOOLEAN      NOT NULL DEFAULT false,
    deletion_timestamp TIMESTAMPTZ,
    finalizers         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name, version)
);
CREATE UNIQUE INDEX IF NOT EXISTS v1alpha1_providers_latest_version  ON v1alpha1.providers (namespace, name) WHERE is_latest_version;
CREATE INDEX IF NOT EXISTS v1alpha1_providers_labels_gin      ON v1alpha1.providers USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_providers_spec_gin        ON v1alpha1.providers USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_providers_updated_at_desc ON v1alpha1.providers (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_providers_terminating    ON v1alpha1.providers (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER providers_set_updated_at  BEFORE UPDATE ON v1alpha1.providers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER providers_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.providers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_providers_status');

CREATE TABLE IF NOT EXISTS v1alpha1.deployments (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    version            VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version  BOOLEAN      NOT NULL DEFAULT false,
    deletion_timestamp TIMESTAMPTZ,
    finalizers         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name, version)
);
CREATE UNIQUE INDEX IF NOT EXISTS v1alpha1_deployments_latest_version  ON v1alpha1.deployments (namespace, name) WHERE is_latest_version;
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_labels_gin      ON v1alpha1.deployments USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_spec_gin        ON v1alpha1.deployments USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_updated_at_desc ON v1alpha1.deployments (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_deployments_terminating    ON v1alpha1.deployments (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER deployments_set_updated_at  BEFORE UPDATE ON v1alpha1.deployments  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER deployments_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.deployments  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_deployments_status');

-- -----------------------------------------------------------------------------
-- Seed: default providers so deployments can reference them out-of-the-box.
-- Seeded in the `default` namespace.
-- -----------------------------------------------------------------------------

INSERT INTO v1alpha1.providers (namespace, name, version, spec, is_latest_version)
VALUES
    ('default', 'local',              'v1', '{"platform":"local"}'::jsonb,      true),
    ('default', 'kubernetes-default', 'v1', '{"platform":"kubernetes"}'::jsonb, true)
ON CONFLICT (namespace, name, version) DO NOTHING;
