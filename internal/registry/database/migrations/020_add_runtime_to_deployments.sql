-- Add runtime column to deployments to track local vs kubernetes targets

ALTER TABLE deployments
ADD COLUMN IF NOT EXISTS runtime VARCHAR(50) NOT NULL DEFAULT 'local';

ALTER TABLE deployments
ADD CONSTRAINT check_deployment_runtime_valid
CHECK (runtime IN ('local', 'kubernetes'));

CREATE INDEX IF NOT EXISTS idx_deployments_runtime ON deployments (runtime);

COMMENT ON COLUMN deployments.runtime IS 'Deployment runtime target: local or kubernetes';
