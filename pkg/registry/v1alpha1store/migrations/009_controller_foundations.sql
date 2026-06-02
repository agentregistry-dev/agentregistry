-- Controller foundations for the Deployment reconciliation model.
--
-- `control_plane_events` is a durable invalidation cursor, not audit history.
-- Source-row writes append compact identity events and emit one coarse wakeup
-- notification. Controllers replay events by revision and re-read canonical
-- source tables; if retained events no longer cover a checkpoint, they must
-- full-reconcile from canonical tables.

CREATE TABLE IF NOT EXISTS v1alpha1.control_plane_events (
    revision     BIGSERIAL    PRIMARY KEY,
    kind         TEXT         NOT NULL,
    namespace    VARCHAR(255) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    tag          VARCHAR(255) NOT NULL DEFAULT '',
    uid          UUID         NOT NULL,
    generation   BIGINT       NOT NULL,
    op           TEXT         NOT NULL CHECK (op IN ('insert', 'update', 'delete')),
    committed_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS control_plane_events_committed_at
    ON v1alpha1.control_plane_events (committed_at, revision);
CREATE INDEX IF NOT EXISTS control_plane_events_identity
    ON v1alpha1.control_plane_events (kind, namespace, name, tag, generation);

CREATE OR REPLACE FUNCTION v1alpha1.record_control_plane_event()
RETURNS TRIGGER AS $$
DECLARE
    event_kind TEXT := TG_ARGV[0];
    event_op TEXT;
    event_revision BIGINT;
    row_json JSONB;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Status-only writes already have their own public watch channel. They
        -- do not change desired source state and must not wake controllers.
        IF NEW.spec IS NOT DISTINCT FROM OLD.spec
           AND NEW.labels IS NOT DISTINCT FROM OLD.labels
           AND NEW.annotations IS NOT DISTINCT FROM OLD.annotations
           AND NEW.deletion_timestamp IS NOT DISTINCT FROM OLD.deletion_timestamp
           AND to_jsonb(NEW)->'finalizers' IS NOT DISTINCT FROM to_jsonb(OLD)->'finalizers' THEN
            RETURN NEW;
        END IF;
        event_op := 'update';
        row_json := to_jsonb(NEW);
    ELSIF TG_OP = 'DELETE' THEN
        event_op := 'delete';
        row_json := to_jsonb(OLD);
    ELSE
        event_op := 'insert';
        row_json := to_jsonb(NEW);
    END IF;

    INSERT INTO v1alpha1.control_plane_events (
        kind,
        namespace,
        name,
        tag,
        uid,
        generation,
        op
    ) VALUES (
        event_kind,
        row_json->>'namespace',
        row_json->>'name',
        COALESCE(row_json->>'tag', ''),
        (row_json->>'uid')::uuid,
        (row_json->>'generation')::bigint,
        event_op
    )
    RETURNING revision INTO event_revision;

    PERFORM pg_notify(
        'v1alpha1_control_plane_changed',
        json_build_object('revision', event_revision)::text
    );

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- This explicit trigger roster is scoped to the OSS Deployment-first
-- controller slice. Skill and Prompt events are retained for future
-- dependency-aware controllers, but the Deployment controller ignores them
-- today.
CREATE OR REPLACE TRIGGER agents_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON v1alpha1.agents
    FOR EACH ROW EXECUTE FUNCTION v1alpha1.record_control_plane_event('Agent');
CREATE OR REPLACE TRIGGER mcp_servers_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON v1alpha1.mcp_servers
    FOR EACH ROW EXECUTE FUNCTION v1alpha1.record_control_plane_event('MCPServer');
CREATE OR REPLACE TRIGGER skills_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON v1alpha1.skills
    FOR EACH ROW EXECUTE FUNCTION v1alpha1.record_control_plane_event('Skill');
CREATE OR REPLACE TRIGGER prompts_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON v1alpha1.prompts
    FOR EACH ROW EXECUTE FUNCTION v1alpha1.record_control_plane_event('Prompt');
CREATE OR REPLACE TRIGGER runtimes_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON v1alpha1.runtimes
    FOR EACH ROW EXECUTE FUNCTION v1alpha1.record_control_plane_event('Runtime');
CREATE OR REPLACE TRIGGER deployments_control_plane_event
    AFTER INSERT OR UPDATE OR DELETE ON v1alpha1.deployments
    FOR EACH ROW EXECUTE FUNCTION v1alpha1.record_control_plane_event('Deployment');
