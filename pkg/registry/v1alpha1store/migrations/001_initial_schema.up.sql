-- 001_initial_schema collapses the previous 001..008 migration
-- sequence into the post-007/post-008 final-state schema. Every
-- identifier is unqualified — the runtime schema (default
-- `agentregistry`) is set via golang-migrate's
-- migratepgx.Config{SchemaName: ...}, so identifiers resolve through
-- search_path.
--
-- Idempotent: every CREATE uses IF NOT EXISTS or OR REPLACE. Re-running
-- against a partially-applied schema is safe.

CREATE OR REPLACE FUNCTION set_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION notify_status_change() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
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
            'namespace', OLD.namespace,
            'name', OLD.name,
            'tag', to_jsonb(OLD)->>'tag');
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
        'namespace', NEW.namespace,
        'name', NEW.name,
        'tag', to_jsonb(NEW)->>'tag');
    PERFORM pg_notify(channel, payload::text);
    RETURN NEW;
END;
$$;

CREATE TABLE IF NOT EXISTS agents (
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

CREATE TABLE IF NOT EXISTS mcp_servers (
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

CREATE TABLE IF NOT EXISTS skills (
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

CREATE TABLE IF NOT EXISTS prompts (
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

CREATE TABLE IF NOT EXISTS runtimes (
    namespace character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    uid uuid DEFAULT gen_random_uuid() NOT NULL,
    generation bigint DEFAULT 1 NOT NULL,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    spec jsonb NOT NULL,
    status jsonb DEFAULT '{}'::jsonb NOT NULL,
    deletion_timestamp timestamp with time zone,
    finalizers jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (namespace, name)
);

CREATE TABLE IF NOT EXISTS deployments (
    namespace character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    uid uuid DEFAULT gen_random_uuid() NOT NULL,
    generation bigint DEFAULT 1 NOT NULL,
    labels jsonb DEFAULT '{}'::jsonb NOT NULL,
    annotations jsonb DEFAULT '{}'::jsonb NOT NULL,
    spec jsonb NOT NULL,
    status jsonb DEFAULT '{}'::jsonb NOT NULL,
    deletion_timestamp timestamp with time zone,
    finalizers jsonb DEFAULT '[]'::jsonb NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    PRIMARY KEY (namespace, name)
);

CREATE INDEX IF NOT EXISTS agents_labels_gin ON agents USING gin (labels);
CREATE INDEX IF NOT EXISTS agents_name_tag_updated_desc ON agents USING btree (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS agents_spec_gin ON agents USING gin (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS agents_terminating ON agents USING btree (deletion_timestamp) WHERE (deletion_timestamp IS NOT NULL);
CREATE INDEX IF NOT EXISTS agents_updated_at_desc ON agents USING btree (updated_at DESC);

CREATE INDEX IF NOT EXISTS mcp_servers_labels_gin ON mcp_servers USING gin (labels);
CREATE INDEX IF NOT EXISTS mcp_servers_name_tag_updated_desc ON mcp_servers USING btree (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS mcp_servers_spec_gin ON mcp_servers USING gin (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS mcp_servers_terminating ON mcp_servers USING btree (deletion_timestamp) WHERE (deletion_timestamp IS NOT NULL);
CREATE INDEX IF NOT EXISTS mcp_servers_updated_at_desc ON mcp_servers USING btree (updated_at DESC);

CREATE INDEX IF NOT EXISTS skills_labels_gin ON skills USING gin (labels);
CREATE INDEX IF NOT EXISTS skills_name_tag_updated_desc ON skills USING btree (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS skills_spec_gin ON skills USING gin (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS skills_terminating ON skills USING btree (deletion_timestamp) WHERE (deletion_timestamp IS NOT NULL);
CREATE INDEX IF NOT EXISTS skills_updated_at_desc ON skills USING btree (updated_at DESC);

CREATE INDEX IF NOT EXISTS prompts_labels_gin ON prompts USING gin (labels);
CREATE INDEX IF NOT EXISTS prompts_name_tag_updated_desc ON prompts USING btree (namespace, name, updated_at DESC, tag);
CREATE INDEX IF NOT EXISTS prompts_spec_gin ON prompts USING gin (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS prompts_terminating ON prompts USING btree (deletion_timestamp) WHERE (deletion_timestamp IS NOT NULL);
CREATE INDEX IF NOT EXISTS prompts_updated_at_desc ON prompts USING btree (updated_at DESC);

CREATE INDEX IF NOT EXISTS runtimes_labels_gin ON runtimes USING gin (labels);
CREATE INDEX IF NOT EXISTS runtimes_spec_gin ON runtimes USING gin (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS runtimes_terminating ON runtimes USING btree (deletion_timestamp) WHERE (deletion_timestamp IS NOT NULL);
CREATE INDEX IF NOT EXISTS runtimes_updated_at_desc ON runtimes USING btree (updated_at DESC);

CREATE INDEX IF NOT EXISTS deployments_labels_gin ON deployments USING gin (labels);
CREATE INDEX IF NOT EXISTS deployments_spec_gin ON deployments USING gin (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS deployments_terminating ON deployments USING btree (deletion_timestamp) WHERE (deletion_timestamp IS NOT NULL);
CREATE INDEX IF NOT EXISTS deployments_updated_at_desc ON deployments USING btree (updated_at DESC);

CREATE OR REPLACE TRIGGER agents_set_updated_at      BEFORE UPDATE                          ON agents      FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER agents_notify_status       AFTER  INSERT OR UPDATE OR DELETE      ON agents      FOR EACH ROW EXECUTE FUNCTION notify_status_change('agents_status');

CREATE OR REPLACE TRIGGER mcp_servers_set_updated_at BEFORE UPDATE                          ON mcp_servers FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER mcp_servers_notify_status  AFTER  INSERT OR UPDATE OR DELETE      ON mcp_servers FOR EACH ROW EXECUTE FUNCTION notify_status_change('mcp_servers_status');

CREATE OR REPLACE TRIGGER skills_set_updated_at      BEFORE UPDATE                          ON skills      FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER skills_notify_status       AFTER  INSERT OR UPDATE OR DELETE      ON skills      FOR EACH ROW EXECUTE FUNCTION notify_status_change('skills_status');

CREATE OR REPLACE TRIGGER prompts_set_updated_at     BEFORE UPDATE                          ON prompts     FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER prompts_notify_status      AFTER  INSERT OR UPDATE OR DELETE      ON prompts     FOR EACH ROW EXECUTE FUNCTION notify_status_change('prompts_status');

CREATE OR REPLACE TRIGGER runtimes_set_updated_at    BEFORE UPDATE                          ON runtimes    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER runtimes_notify_status     AFTER  INSERT OR UPDATE OR DELETE      ON runtimes    FOR EACH ROW EXECUTE FUNCTION notify_status_change('runtimes_status');

CREATE OR REPLACE TRIGGER deployments_set_updated_at BEFORE UPDATE                          ON deployments FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER deployments_notify_status  AFTER  INSERT OR UPDATE OR DELETE      ON deployments FOR EACH ROW EXECUTE FUNCTION notify_status_change('deployments_status');

-- Seed default runtimes so deployments can reference them out-of-the-box.
-- ON CONFLICT DO NOTHING keeps re-applies idempotent.
INSERT INTO runtimes (namespace, name, spec)
VALUES
    ('default', 'local',              '{"type":"Local"}'::jsonb),
    ('default', 'kubernetes-default', '{"type":"Kubernetes"}'::jsonb)
ON CONFLICT (namespace, name) DO NOTHING;
