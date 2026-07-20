-- Models: admin-owned model definitions (provider-scoped identity
-- plus platform-owned auth/endpoint posture). A mutable-object kind keyed by
-- (namespace, name): auth/endpoint edits are routine config mutations, not new
-- versions. Wires the standard updated-at, status-notify, and control-plane
-- event triggers used by mutable resources.

CREATE TABLE IF NOT EXISTS models (
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

CREATE INDEX IF NOT EXISTS models_labels_gin ON models USING gin (labels);
CREATE INDEX IF NOT EXISTS models_spec_gin ON models USING gin (spec jsonb_path_ops);
CREATE INDEX IF NOT EXISTS models_terminating ON models USING btree (deletion_timestamp) WHERE (deletion_timestamp IS NOT NULL);
CREATE INDEX IF NOT EXISTS models_updated_at_desc ON models USING btree (updated_at DESC);

CREATE OR REPLACE TRIGGER models_set_updated_at
    BEFORE UPDATE ON models
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE OR REPLACE TRIGGER models_notify_status
    AFTER INSERT OR UPDATE OR DELETE ON models
    FOR EACH ROW EXECUTE FUNCTION notify_status_change('models_status');
CREATE OR REPLACE TRIGGER models_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON models
    FOR EACH ROW EXECUTE FUNCTION record_control_plane_event('Model');
