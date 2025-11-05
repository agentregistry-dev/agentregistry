-- Create skills table mirroring servers structure
-- Each row represents a specific version of a skill identified by (skill_name, version)

CREATE TABLE IF NOT EXISTS skills (
    skill_name    VARCHAR(255) NOT NULL,
    version       VARCHAR(255) NOT NULL,
    status        VARCHAR(50)  NOT NULL DEFAULT 'active',
    published_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    is_latest     BOOLEAN NOT NULL DEFAULT true,

    -- Complete SkillJSON payload as JSONB (same shape as ServerJSON for now)
    value         JSONB NOT NULL,

    CONSTRAINT skills_pkey PRIMARY KEY (skill_name, version)
);

-- Indexes to mirror servers performance characteristics
CREATE INDEX IF NOT EXISTS idx_skills_name ON skills (skill_name);
CREATE INDEX IF NOT EXISTS idx_skills_name_version ON skills (skill_name, version);
CREATE INDEX IF NOT EXISTS idx_skills_latest ON skills (skill_name, is_latest) WHERE is_latest = true;
CREATE INDEX IF NOT EXISTS idx_skills_status ON skills (status);
CREATE INDEX IF NOT EXISTS idx_skills_published_at ON skills (published_at DESC);
CREATE INDEX IF NOT EXISTS idx_skills_updated_at ON skills (updated_at DESC);

-- Trigger and function to auto-update updated_at on modification
CREATE OR REPLACE FUNCTION update_skills_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS trg_update_skills_updated_at ON skills;
CREATE TRIGGER trg_update_skills_updated_at
    BEFORE UPDATE ON skills
    FOR EACH ROW
    EXECUTE FUNCTION update_skills_updated_at();

-- Ensure only one version per skill is marked latest
CREATE UNIQUE INDEX IF NOT EXISTS idx_unique_latest_per_skill
ON skills (skill_name)
WHERE is_latest = true;

-- Basic integrity checks similar to servers
ALTER TABLE skills ADD CONSTRAINT check_skill_status_valid
CHECK (status IN ('active', 'deprecated', 'deleted'));

ALTER TABLE skills ADD CONSTRAINT check_skill_name_format
CHECK (skill_name ~ '^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]/[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$');

ALTER TABLE skills ADD CONSTRAINT check_skill_version_not_empty
CHECK (length(trim(version)) > 0);

