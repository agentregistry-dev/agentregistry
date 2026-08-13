-- Restore the v0.4.0 Kubernetes runtime seed when the name remains available.
INSERT INTO runtimes (namespace, name, spec)
VALUES ('default', 'kubernetes-default', '{"type":"Kubernetes"}'::jsonb)
ON CONFLICT (namespace, name) DO NOTHING;
