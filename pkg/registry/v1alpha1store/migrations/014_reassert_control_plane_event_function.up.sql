-- Re-assert record_control_plane_event() at its current definition.
--
-- The Plugin/Skill resolvedSource exception below was added by editing
-- migration 009 after it had already been applied to existing databases, so
-- those databases still run the original function, which skips ALL
-- status-only updates. On such a database a Skill/Plugin resolve (a
-- status-only write that sets status.resolvedSource) emits no control-plane
-- event, and Deployments blocked on that resolve are never requeued when it
-- lands. Re-running CREATE OR REPLACE here converges every database to the
-- definition migration 009 now describes; on databases migrated after that
-- edit this is a no-op.

CREATE OR REPLACE FUNCTION record_control_plane_event()
RETURNS TRIGGER AS $$
DECLARE
    event_kind TEXT := TG_ARGV[0];
    event_op TEXT;
    event_revision BIGINT;
    row_json JSONB;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Status-only writes already have their own public watch channel. They
        -- do not usually change desired source state and must not wake
        -- controllers. Plugin/Skill resolvedSource is the narrow exception:
        -- harness Deployments consume that material pin.
        IF NEW.spec = OLD.spec
           AND NEW.labels = OLD.labels
           AND NEW.annotations = OLD.annotations
           AND (
               NEW.deletion_timestamp = OLD.deletion_timestamp
               OR (NEW.deletion_timestamp IS NULL AND OLD.deletion_timestamp IS NULL)
           )
           AND COALESCE(to_jsonb(NEW)->'finalizers', '[]'::jsonb) =
               COALESCE(to_jsonb(OLD)->'finalizers', '[]'::jsonb) THEN
            IF NOT (
                event_kind IN ('Plugin', 'Skill')
                AND COALESCE(NEW.status->'resolvedSource', 'null'::jsonb)
                    IS DISTINCT FROM COALESCE(OLD.status->'resolvedSource', 'null'::jsonb)
            ) THEN
                RETURN NEW;
            END IF;
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

    INSERT INTO control_plane_events (
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
