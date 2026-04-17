package models

import "time"

// RegistryRef is the unified reference type for all agent dependencies
// (MCP servers, skills, prompts) in the declarative agent spec.
// Resources are looked up in the registry by (Name, Version) which matches
// the composite primary keys used across all registry tables.
type RegistryRef struct {
	Name    string `json:"name" yaml:"name"`                           // registry resource name (e.g. "myorg/weather-mcp")
	Version string `json:"version,omitempty" yaml:"version,omitempty"` // version; empty = resolve to latest
}

// AgentManifest represents the agent project configuration and metadata.
type AgentManifest struct {
	Name              string          `yaml:"agentName" json:"name"`
	Image             string          `yaml:"image" json:"image"`
	Language          string          `yaml:"language" json:"language"`
	Framework         string          `yaml:"framework" json:"framework"`
	ModelProvider     string          `yaml:"modelProvider" json:"modelProvider"`
	ModelName         string          `yaml:"modelName" json:"modelName"`
	Description       string          `yaml:"description" json:"description"`
	Version           string          `yaml:"version,omitempty" json:"version,omitempty"`
	TelemetryEndpoint string          `yaml:"telemetryEndpoint,omitempty" json:"telemetryEndpoint,omitempty"`
	McpServers        []McpServerType `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`
	Skills            []SkillRef      `yaml:"skills,omitempty" json:"skills,omitempty"`
	Prompts           []PromptRef     `yaml:"prompts,omitempty" json:"prompts,omitempty"`
	UpdatedAt         time.Time       `yaml:"updatedAt,omitempty" json:"updatedAt,omitempty"`
}

// SkillRef represents a skill reference in the agent manifest.
type SkillRef struct {
	// Name is the local name for the skill in this agent project.
	Name string `yaml:"name" json:"name"`
	// Image is a Docker image containing the skill (for image type).
	Image string `yaml:"image,omitempty" json:"image,omitempty"`
	// RegistryURL is the registry URL for pulling the skill (for registry type).
	RegistryURL string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	// RegistrySkillName is the skill name in the registry.
	RegistrySkillName string `yaml:"registrySkillName,omitempty" json:"registrySkillName,omitempty"`
	// RegistrySkillVersion is the version of the skill to pull.
	RegistrySkillVersion string `yaml:"registrySkillVersion,omitempty" json:"registrySkillVersion,omitempty"`
}

// PromptRef represents a prompt reference in the agent manifest.
type PromptRef struct {
	// Name is the local name for the prompt in this agent project.
	Name string `yaml:"name" json:"name"`
	// RegistryURL is the registry URL for pulling the prompt (for registry type).
	RegistryURL string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	// RegistryPromptName is the prompt name in the registry.
	RegistryPromptName string `yaml:"registryPromptName,omitempty" json:"registryPromptName,omitempty"`
	// RegistryPromptVersion is the version of the prompt to pull.
	RegistryPromptVersion string `yaml:"registryPromptVersion,omitempty" json:"registryPromptVersion,omitempty"`
}

// McpServerType represents a single MCP server configuration.
// New declarative format: only Name + Version are set (Name is the registry server name).
// Legacy format: Type is set ("remote", "command", or "registry") with type-specific fields.
type McpServerType struct {
	// Name is the registry server name (new format) or local display name (legacy format).
	Name string `yaml:"name" json:"name"`
	// Version is the server version for the new declarative format.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	// TODO(legacy): remove fields below once declarative API is the only supported path
	// Type is the MCP server type -- remote, command, registry (legacy format only).
	Type    string            `yaml:"type,omitempty" json:"type,omitempty"`
	Image   string            `yaml:"image,omitempty" json:"image,omitempty"`
	Build   string            `yaml:"build,omitempty" json:"build,omitempty"`
	Command string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     []string          `yaml:"env,omitempty" json:"env,omitempty"`
	URL     string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	// Registry MCP server fields -- these are translated into the appropriate fields above when the agent is ran or deployed
	RegistryURL                string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	RegistryServerName         string `yaml:"registryServerName,omitempty" json:"registryServerName,omitempty"`
	RegistryServerVersion      string `yaml:"registryServerVersion,omitempty" json:"registryServerVersion,omitempty"`
	RegistryServerPreferRemote bool   `yaml:"registryServerPreferRemote,omitempty" json:"registryServerPreferRemote,omitempty"`
}

// IsLegacyFormat returns true if this MCP server entry uses the legacy format
// (Type or RegistryServerName set), false if it uses the new RegistryRef format.
func (m *McpServerType) IsLegacyFormat() bool {
	return m.Type != "" || m.RegistryServerName != ""
}

// ToRegistryRef converts a new-format McpServerType entry to a RegistryRef.
// Returns nil if this is a legacy-format entry.
func (m *McpServerType) ToRegistryRef() *RegistryRef {
	if m.IsLegacyFormat() {
		return nil
	}
	return &RegistryRef{Name: m.Name, Version: m.Version}
}

// ExtractMCPServerRefs extracts RegistryRef entries from the manifest's McpServers list.
// Only new-format entries (no Type or RegistryServerName set) are included.
func (am *AgentManifest) ExtractMCPServerRefs() []RegistryRef {
	var refs []RegistryRef
	for _, s := range am.McpServers {
		if ref := s.ToRegistryRef(); ref != nil {
			refs = append(refs, *ref)
		}
	}
	return refs
}
