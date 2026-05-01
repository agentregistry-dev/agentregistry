-- 005_remote_resources.sql
--
-- Adds the new top-level kind for already-running MCP endpoints:
--   v1alpha1.remote_mcp_servers — peer of v1alpha1.mcp_servers, points at a
--                                 running MCP endpoint (no lifecycle).
--
-- Schema mirrors the other v1alpha1 tables: same envelope columns, same
-- indexes, same triggers. The Kubernetes-style envelope keeps the row
-- shape identical across every kind so the generic Store works without
-- per-kind branching.
--
-- This migration only creates the table. Existing pre-v1alpha1 demo data is
-- intentionally not translated; operators should start this API shape from a
-- fresh database.

CREATE TABLE IF NOT EXISTS v1alpha1.remote_mcp_servers (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    version            VARCHAR(255) NOT NULL,
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
CREATE UNIQUE INDEX IF NOT EXISTS v1alpha1_remote_mcp_servers_latest_version  ON v1alpha1.remote_mcp_servers (namespace, name) WHERE is_latest_version;
CREATE INDEX IF NOT EXISTS v1alpha1_remote_mcp_servers_labels_gin      ON v1alpha1.remote_mcp_servers USING GIN (labels);
CREATE INDEX IF NOT EXISTS v1alpha1_remote_mcp_servers_spec_gin        ON v1alpha1.remote_mcp_servers USING GIN (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS v1alpha1_remote_mcp_servers_updated_at_desc ON v1alpha1.remote_mcp_servers (updated_at DESC);
CREATE INDEX IF NOT EXISTS v1alpha1_remote_mcp_servers_terminating     ON v1alpha1.remote_mcp_servers (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;
CREATE OR REPLACE TRIGGER remote_mcp_servers_set_updated_at  BEFORE UPDATE ON v1alpha1.remote_mcp_servers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.set_updated_at();
CREATE OR REPLACE TRIGGER remote_mcp_servers_notify_status   AFTER  INSERT OR UPDATE OR DELETE ON v1alpha1.remote_mcp_servers  FOR EACH ROW EXECUTE FUNCTION v1alpha1.notify_status_change('v1alpha1_remote_mcp_servers_status');
