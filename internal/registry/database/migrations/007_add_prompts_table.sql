-- =============================================================================
-- PROMPTS TABLE
-- =============================================================================

CREATE TABLE prompts (
    -- Primary identifiers
    prompt_name VARCHAR(255) NOT NULL,
    version VARCHAR(255) NOT NULL,

    -- Status and timestamps
    status VARCHAR(50) NOT NULL DEFAULT 'active',
    published_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_latest BOOLEAN NOT NULL DEFAULT true,

    -- Complete PromptJSON payload as JSONB
    value JSONB NOT NULL,

    -- Primary key
    CONSTRAINT prompts_pkey PRIMARY KEY (prompt_name, version)
);

-- Indexes for prompts
CREATE INDEX idx_prompts_name ON prompts (prompt_name);
CREATE INDEX idx_prompts_name_version ON prompts (prompt_name, version);
CREATE INDEX idx_prompts_latest ON prompts (prompt_name, is_latest) WHERE is_latest = true;
CREATE INDEX idx_prompts_status ON prompts (status);
CREATE INDEX idx_prompts_published_at ON prompts (published_at DESC);
CREATE INDEX idx_prompts_updated_at ON prompts (updated_at DESC);

-- Ensure only one version per prompt is marked as latest
CREATE UNIQUE INDEX idx_unique_latest_per_prompt ON prompts (prompt_name) WHERE is_latest = true;

-- Trigger function to auto-update updated_at
CREATE OR REPLACE FUNCTION update_prompts_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_update_prompts_updated_at
    BEFORE UPDATE ON prompts
    FOR EACH ROW
    EXECUTE FUNCTION update_prompts_updated_at();

-- Check constraints
ALTER TABLE prompts ADD CONSTRAINT check_prompt_status_valid
    CHECK (status IN ('active', 'deprecated', 'deleted'));

ALTER TABLE prompts ADD CONSTRAINT check_prompt_name_format
    CHECK (prompt_name ~ '^[a-zA-Z0-9_-]+$');

ALTER TABLE prompts ADD CONSTRAINT check_prompt_version_not_empty
    CHECK (length(trim(version)) > 0);
