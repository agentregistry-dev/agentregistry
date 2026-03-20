-- Seed the default OpenShell provider so it is available out of the box,
-- matching the pattern established for local and kubernetes-default.

INSERT INTO providers (id, name, platform, config)
VALUES ('openshell-default', 'OpenShell Default', 'openshell', '{}'::jsonb)
ON CONFLICT (id) DO NOTHING;
