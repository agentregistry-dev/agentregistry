-- Platform validity is adapter-driven and should not be constrained at DB level.

ALTER TABLE deployments
    DROP CONSTRAINT IF EXISTS check_deployment_provider_valid;

ALTER TABLE providers
    DROP CONSTRAINT IF EXISTS check_provider_platform_valid;
