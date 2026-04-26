-- 004_notify_payload_discrete.sql
--
-- Replace the notify_status_change trigger function so its payload
-- carries the (namespace, name, version) identity as three discrete JSON
-- fields instead of one concatenated "ns/name/version" string.
--
-- Motivation: resource names may contain `/` (the nameRegex explicitly
-- supports DNS-subdomain-style names like `ai.exa/exa`), which makes the
-- old concatenated id payload ambiguous to any strings.Split consumer.
-- No consumer parses the payload yet — Phase 2 KRT is still pending —
-- so fix the wire now while the cost is a trigger swap and nothing
-- downstream needs migrating.
--
-- New payload shape:
--   {"op":"INSERT|UPDATE|DELETE","namespace":"<ns>","name":"<name>","version":"<version>"}
--
-- `id` is no longer emitted. Consumers that previously parsed it should
-- read the three discrete fields directly.

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
            'namespace', OLD.namespace,
            'name', OLD.name,
            'version', OLD.version);
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
        'version', NEW.version);
    PERFORM pg_notify(channel, payload::text);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
