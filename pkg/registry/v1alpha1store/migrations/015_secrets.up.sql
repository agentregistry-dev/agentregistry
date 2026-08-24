CREATE TABLE IF NOT EXISTS secrets (
    namespace          VARCHAR(255) NOT NULL,
    name               VARCHAR(255) NOT NULL,
    uid                UUID         NOT NULL DEFAULT gen_random_uuid(),
    generation         BIGINT       NOT NULL DEFAULT 1,
    labels             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    annotations        JSONB        NOT NULL DEFAULT '{}'::jsonb,
    spec               JSONB        NOT NULL,
    status             JSONB        NOT NULL DEFAULT '{}'::jsonb,
    deletion_timestamp TIMESTAMPTZ,
    finalizers         JSONB        NOT NULL DEFAULT '[]'::jsonb,
    created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name),
    CONSTRAINT secrets_no_plaintext_values CHECK (
        NOT (spec ? 'data') AND NOT (spec ? 'stringData')
    )
);

CREATE INDEX IF NOT EXISTS secrets_labels_gin ON secrets USING GIN (labels);
CREATE INDEX IF NOT EXISTS secrets_updated_at_desc ON secrets (updated_at DESC);
CREATE INDEX IF NOT EXISTS secrets_terminating ON secrets (deletion_timestamp) WHERE deletion_timestamp IS NOT NULL;

CREATE OR REPLACE TRIGGER secrets_set_updated_at
    BEFORE UPDATE ON secrets
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

CREATE TABLE IF NOT EXISTS secret_payloads (
    namespace  VARCHAR(255) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    ciphertext BYTEA        NOT NULL,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    PRIMARY KEY (namespace, name)
);

CREATE OR REPLACE TRIGGER secret_payloads_set_updated_at
    BEFORE UPDATE ON secret_payloads
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
