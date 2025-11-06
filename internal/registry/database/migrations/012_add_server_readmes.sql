BEGIN;

CREATE TABLE IF NOT EXISTS server_readmes (
    server_name VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,
    content BYTEA NOT NULL,
    content_type TEXT NOT NULL DEFAULT 'text/markdown',
    size_bytes INTEGER NOT NULL,
    sha256 BYTEA NOT NULL,
    fetched_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    PRIMARY KEY (server_name, version),
    CONSTRAINT fk_server_readmes_server FOREIGN KEY (server_name, version)
        REFERENCES servers(server_name, version)
        ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_server_readmes_server_name_version
    ON server_readmes (server_name, version);

COMMIT;

