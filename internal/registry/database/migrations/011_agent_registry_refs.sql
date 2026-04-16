-- Add registry ref columns for declarative agent dependencies.
-- These store the canonical RegistryRef entries extracted during arctl apply,
-- separate from the value JSONB which retains the full manifest for backward compat.

ALTER TABLE agents ADD COLUMN mcp_server_refs JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE agents ADD COLUMN skill_refs JSONB NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE agents ADD COLUMN prompt_refs JSONB NOT NULL DEFAULT '[]'::jsonb;

-- GIN indexes for containment queries (e.g. "which agents reference this MCP server?")
CREATE INDEX idx_agents_mcp_server_refs ON agents USING GIN (mcp_server_refs);
CREATE INDEX idx_agents_skill_refs ON agents USING GIN (skill_refs);
CREATE INDEX idx_agents_prompt_refs ON agents USING GIN (prompt_refs);
