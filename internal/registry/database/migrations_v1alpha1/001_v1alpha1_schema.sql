-- v1alpha1 schema: every resource uses the same envelope (apiVersion +
-- metadata + spec + status). Metadata fields (name, version, labels,
-- generation, created_at, updated_at) are promoted to real columns; spec and
-- status stay JSONB. (name, version) is the composite primary key for every
-- kind. Reverse-lookup queries run off a GIN index on the spec JSONB.
--
-- This migration is intentionally additive in the PR 2 window: it lives in a
-- separate embed.FS (migrations_v1alpha1) that is not wired to the production
-- migration runner. It exists so the generic Store unit tests can spin up a
-- fresh schema. PR 3 flips the production runner over to this schema and
-- deletes the legacy migrations.

-- -----------------------------------------------------------------------------
-- Shared helpers
-- -----------------------------------------------------------------------------

CREATE OR REPLACE FUNCTION set_updated_at()
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
CREATE OR REPLACE FUNCTION notify_status_change()
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
        payload := json_build_object('op', op, 'id', OLD.name || '/' || OLD.version);
        PERFORM pg_notify(channel, payload::text);
        RETURN OLD;
    ELSE
        op := 'UPDATE';
        IF NEW.status::text = OLD.status::text THEN
            RETURN NEW;
        END IF;
    END IF;
    payload := json_build_object('op', op, 'id', NEW.name || '/' || NEW.version);
    PERFORM pg_notify(channel, payload::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- -----------------------------------------------------------------------------
-- Per-kind tables: identical shape across agents, mcp_servers, skills,
-- prompts, providers, deployments.
-- -----------------------------------------------------------------------------

CREATE TABLE agents (
    name              VARCHAR(255) NOT NULL,
    version           VARCHAR(255) NOT NULL,
    generation        BIGINT       NOT NULL DEFAULT 1,
    labels            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec              JSONB        NOT NULL,
    status            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version BOOLEAN      NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, version)
);
CREATE UNIQUE INDEX agents_latest_version  ON agents (name) WHERE is_latest_version;
CREATE        INDEX agents_labels_gin      ON agents USING GIN (labels);
CREATE        INDEX agents_spec_gin        ON agents USING GIN (spec jsonb_path_ops);
CREATE        INDEX agents_updated_at_desc ON agents (updated_at DESC);

CREATE TRIGGER agents_set_updated_at  BEFORE UPDATE ON agents  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER agents_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON agents  FOR EACH ROW EXECUTE FUNCTION notify_status_change('agents_status');

CREATE TABLE mcp_servers (
    name              VARCHAR(255) NOT NULL,
    version           VARCHAR(255) NOT NULL,
    generation        BIGINT       NOT NULL DEFAULT 1,
    labels            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec              JSONB        NOT NULL,
    status            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version BOOLEAN      NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, version)
);
CREATE UNIQUE INDEX mcp_servers_latest_version  ON mcp_servers (name) WHERE is_latest_version;
CREATE        INDEX mcp_servers_labels_gin      ON mcp_servers USING GIN (labels);
CREATE        INDEX mcp_servers_spec_gin        ON mcp_servers USING GIN (spec jsonb_path_ops);
CREATE        INDEX mcp_servers_updated_at_desc ON mcp_servers (updated_at DESC);
CREATE TRIGGER mcp_servers_set_updated_at  BEFORE UPDATE ON mcp_servers  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER mcp_servers_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON mcp_servers  FOR EACH ROW EXECUTE FUNCTION notify_status_change('mcp_servers_status');

CREATE TABLE skills (
    name              VARCHAR(255) NOT NULL,
    version           VARCHAR(255) NOT NULL,
    generation        BIGINT       NOT NULL DEFAULT 1,
    labels            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec              JSONB        NOT NULL,
    status            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version BOOLEAN      NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, version)
);
CREATE UNIQUE INDEX skills_latest_version  ON skills (name) WHERE is_latest_version;
CREATE        INDEX skills_labels_gin      ON skills USING GIN (labels);
CREATE        INDEX skills_spec_gin        ON skills USING GIN (spec jsonb_path_ops);
CREATE        INDEX skills_updated_at_desc ON skills (updated_at DESC);
CREATE TRIGGER skills_set_updated_at  BEFORE UPDATE ON skills  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER skills_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON skills  FOR EACH ROW EXECUTE FUNCTION notify_status_change('skills_status');

CREATE TABLE prompts (
    name              VARCHAR(255) NOT NULL,
    version           VARCHAR(255) NOT NULL,
    generation        BIGINT       NOT NULL DEFAULT 1,
    labels            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec              JSONB        NOT NULL,
    status            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version BOOLEAN      NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, version)
);
CREATE UNIQUE INDEX prompts_latest_version  ON prompts (name) WHERE is_latest_version;
CREATE        INDEX prompts_labels_gin      ON prompts USING GIN (labels);
CREATE        INDEX prompts_spec_gin        ON prompts USING GIN (spec jsonb_path_ops);
CREATE        INDEX prompts_updated_at_desc ON prompts (updated_at DESC);
CREATE TRIGGER prompts_set_updated_at  BEFORE UPDATE ON prompts  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER prompts_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON prompts  FOR EACH ROW EXECUTE FUNCTION notify_status_change('prompts_status');

CREATE TABLE providers (
    name              VARCHAR(255) NOT NULL,
    version           VARCHAR(255) NOT NULL,
    generation        BIGINT       NOT NULL DEFAULT 1,
    labels            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec              JSONB        NOT NULL,
    status            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version BOOLEAN      NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, version)
);
CREATE UNIQUE INDEX providers_latest_version  ON providers (name) WHERE is_latest_version;
CREATE        INDEX providers_labels_gin      ON providers USING GIN (labels);
CREATE        INDEX providers_spec_gin        ON providers USING GIN (spec jsonb_path_ops);
CREATE        INDEX providers_updated_at_desc ON providers (updated_at DESC);
CREATE TRIGGER providers_set_updated_at  BEFORE UPDATE ON providers  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER providers_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON providers  FOR EACH ROW EXECUTE FUNCTION notify_status_change('providers_status');

CREATE TABLE deployments (
    name              VARCHAR(255) NOT NULL,
    version           VARCHAR(255) NOT NULL,
    generation        BIGINT       NOT NULL DEFAULT 1,
    labels            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec              JSONB        NOT NULL,
    status            JSONB        NOT NULL DEFAULT '{}'::jsonb,
    is_latest_version BOOLEAN      NOT NULL DEFAULT false,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, version)
);
CREATE UNIQUE INDEX deployments_latest_version  ON deployments (name) WHERE is_latest_version;
CREATE        INDEX deployments_labels_gin      ON deployments USING GIN (labels);
CREATE        INDEX deployments_spec_gin        ON deployments USING GIN (spec jsonb_path_ops);
CREATE        INDEX deployments_updated_at_desc ON deployments (updated_at DESC);
CREATE TRIGGER deployments_set_updated_at  BEFORE UPDATE ON deployments  FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER deployments_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON deployments  FOR EACH ROW EXECUTE FUNCTION notify_status_change('deployments_status');

-- -----------------------------------------------------------------------------
-- Reconciler support (Phase 2 carry-over)
-- -----------------------------------------------------------------------------

-- reconcile_events: audit trail + backoff state for deployment reconciliation.
-- Keyed by deployment (name, version) rather than a UUID so it aligns with the
-- v1alpha1 composite-PK convention. Populated by the reconciler only.
CREATE TABLE reconcile_events (
    id                 BIGSERIAL PRIMARY KEY,
    deployment_name    VARCHAR(255) NOT NULL,
    deployment_version VARCHAR(255) NOT NULL,
    action             VARCHAR(50)  NOT NULL,
    status             VARCHAR(50)  NOT NULL,
    error              TEXT,
    attempts           INTEGER      NOT NULL DEFAULT 0,
    next_retry_at      TIMESTAMPTZ,
    started_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    completed_at       TIMESTAMPTZ,
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    FOREIGN KEY (deployment_name, deployment_version)
        REFERENCES deployments (name, version) ON DELETE CASCADE
);
CREATE INDEX reconcile_events_deployment     ON reconcile_events (deployment_name, deployment_version);
CREATE INDEX reconcile_events_started_at     ON reconcile_events (started_at DESC);
CREATE INDEX reconcile_events_next_retry_at  ON reconcile_events (next_retry_at) WHERE next_retry_at IS NOT NULL;

CREATE TRIGGER reconcile_events_set_updated_at
    BEFORE UPDATE ON reconcile_events
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- Keep only the 10 most recent events per deployment to bound table growth.
CREATE OR REPLACE FUNCTION trim_reconcile_events()
RETURNS TRIGGER AS $$
BEGIN
    DELETE FROM reconcile_events
    WHERE deployment_name = NEW.deployment_name
      AND deployment_version = NEW.deployment_version
      AND id NOT IN (
          SELECT id FROM reconcile_events
          WHERE deployment_name = NEW.deployment_name
            AND deployment_version = NEW.deployment_version
          ORDER BY started_at DESC LIMIT 10
      );
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER reconcile_events_trim
    AFTER INSERT ON reconcile_events
    FOR EACH ROW EXECUTE FUNCTION trim_reconcile_events();

-- -----------------------------------------------------------------------------
-- Enterprise-owned discovery tables (Syncer writes).
-- Recreated fresh; shape unchanged from Phase 2 schema.
-- -----------------------------------------------------------------------------

CREATE TABLE discovered_local (
    id           TEXT PRIMARY KEY,
    provider_id  TEXT NOT NULL,
    kind         VARCHAR(50) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX discovered_local_provider ON discovered_local (provider_id);

CREATE TRIGGER discovered_local_set_updated_at
    BEFORE UPDATE ON discovered_local
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE discovered_kubernetes (
    id           TEXT PRIMARY KEY,
    provider_id  TEXT NOT NULL,
    kind         VARCHAR(50) NOT NULL,
    namespace    VARCHAR(255),
    name         VARCHAR(255) NOT NULL,
    metadata     JSONB NOT NULL DEFAULT '{}'::jsonb,
    discovered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX discovered_kubernetes_provider ON discovered_kubernetes (provider_id);

CREATE TRIGGER discovered_kubernetes_set_updated_at
    BEFORE UPDATE ON discovered_kubernetes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Server README attachments (OSS MCP server detail store).
-- Kept separate because content is BYTEA-adjacent and not part of the
-- hot-path spec.
-- -----------------------------------------------------------------------------

CREATE TABLE mcpserver_readmes (
    name        VARCHAR(255) NOT NULL,
    version     VARCHAR(255) NOT NULL,
    content     BYTEA NOT NULL,
    sha256      VARCHAR(64) NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (name, version),
    FOREIGN KEY (name, version) REFERENCES mcp_servers (name, version) ON DELETE CASCADE
);

CREATE TRIGGER mcpserver_readmes_set_updated_at
    BEFORE UPDATE ON mcpserver_readmes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- -----------------------------------------------------------------------------
-- Seed: default providers so deployments can reference them out-of-the-box.
-- -----------------------------------------------------------------------------

INSERT INTO providers (name, version, spec, is_latest_version)
VALUES
    ('local',              'v1', '{"platform":"local"}'::jsonb,       true),
    ('kubernetes-default', 'v1', '{"platform":"kubernetes"}'::jsonb,  true)
ON CONFLICT (name, version) DO NOTHING;
