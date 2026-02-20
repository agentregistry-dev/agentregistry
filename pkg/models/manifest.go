package models

import "time"

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

// McpServerType represents a single MCP server configuration.
type McpServerType struct {
	// MCP Server Type -- remote, command, registry
	Type    string            `yaml:"type" json:"type"`
	Name    string            `yaml:"name" json:"name"`
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
