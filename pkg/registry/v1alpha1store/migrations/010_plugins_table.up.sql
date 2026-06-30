-- Plugins: self-contained harness plugin bundles (skills + MCP servers + hooks
-- + sub-agents), a content-registry kind keyed by (namespace, name, tag) and
-- immutable by tag. Shape mirrors the skills/mcp_servers content tables and
-- wires the same updated-at, status-notify, and control-plane-event triggers
-- (009) so the controller observes plugin changes like every other kind.

CREATE TABLE IF NOT EXISTS plugins (
    namespace character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    tag character varying(255) NOT NULL,
    uid uuid DEFAULT gen_random_uuid() NOT NULL,
    generation bigint DEFAULT 1 NOT NULL,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    spec jsonb NOT NULL,
    content_hash character(64) NOT NULL,
    status jsonb DEFAULT '{}'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    deletion_timestamp timestamp with time zone,
    PRIMARY KEY (namespace, name, tag)
);

-- list by labels
CREATE INDEX IF NOT EXISTS plugins_labels_gin
    ON plugins USING gin (labels);

-- list live plugin rows
CREATE INDEX IF NOT EXISTS plugins_list_alive
    ON plugins USING btree (namespace, name, tag, updated_at)
    WHERE deletion_timestamp IS NULL;

-- list tags for one plugin
CREATE INDEX IF NOT EXISTS plugins_tags_alive
    ON plugins USING btree (namespace, name, updated_at DESC, tag DESC)
    WHERE deletion_timestamp IS NULL;

-- purge terminating rows
CREATE INDEX IF NOT EXISTS plugins_terminating
    ON plugins USING btree (deletion_timestamp)
    WHERE deletion_timestamp IS NOT NULL;

CREATE OR REPLACE TRIGGER plugins_set_updated_at
    BEFORE UPDATE ON plugins
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER plugins_notify_status
    AFTER INSERT OR UPDATE OR DELETE ON plugins
    FOR EACH ROW EXECUTE FUNCTION notify_status_change('plugins_status');
CREATE OR REPLACE TRIGGER plugins_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON plugins
    FOR EACH ROW EXECUTE FUNCTION record_control_plane_event('Plugin');
