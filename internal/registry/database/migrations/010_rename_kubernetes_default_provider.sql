-- Rename the kubernetes-default provider ID to kubernetes for consistency.

-- Insert the new provider if it doesn't exist yet.
INSERT INTO providers (id, name, platform, config)
VALUES ('kubernetes', 'Kubernetes', 'kubernetes', '{}'::jsonb)
ON CONFLICT (id) DO NOTHING;

-- Migrate any deployments referencing the old ID.
UPDATE deployments
SET provider_id = 'kubernetes'
WHERE provider_id = 'kubernetes-default';

-- Remove the old provider.
DELETE FROM providers WHERE id = 'kubernetes-default';
