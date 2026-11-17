-- Restore the v0.4.0 Local runtime seed when the name remains available.
INSERT INTO runtimes (namespace, name, spec)
VALUES ('default', 'local', '{"type":"Local"}'::jsonb)
ON CONFLICT (namespace, name) DO NOTHING;
