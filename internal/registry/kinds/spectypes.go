// spectypes.go defines the typed spec: blocks for all declarative document kinds.
// Each spec struct carries yaml + json tags so that YAML decode and JSON
// round-trip both work with identical field names. These replace the per-kind
// sub-packages (agent/, skill/, prompt/, mcp/, provider/, deployment/) that
// were previously under this directory.
package kinds

import "github.com/agentregistry-dev/agentregistry/pkg/models"

// ---------------------------------------------------------------------------
// Agent
// ---------------------------------------------------------------------------

// AgentSpec is the typed spec: block for a kind: agent declarative document.
// Fields mirror the AgentJSON wire contract so that typed decode is lossless.
type AgentSpec struct {
	// AgentManifest fields (inlined in AgentJSON)
	Image             string           `yaml:"image,omitempty" json:"image,omitempty"`
	Language          string           `yaml:"language,omitempty" json:"language,omitempty"`
	Framework         string           `yaml:"framework,omitempty" json:"framework,omitempty"`
	ModelProvider     string           `yaml:"modelProvider,omitempty" json:"modelProvider,omitempty"`
	ModelName         string           `yaml:"modelName,omitempty" json:"modelName,omitempty"`
	Description       string           `yaml:"description,omitempty" json:"description,omitempty"`
	TelemetryEndpoint string           `yaml:"telemetryEndpoint,omitempty" json:"telemetryEndpoint,omitempty"`
	McpServers        []AgentMcpServer `yaml:"mcpServers,omitempty" json:"mcpServers,omitempty"`
	Skills            []AgentSkillRef  `yaml:"skills,omitempty" json:"skills,omitempty"`
	Prompts           []AgentPromptRef `yaml:"prompts,omitempty" json:"prompts,omitempty"`

	// AgentJSON top-level fields
	Title      string            `yaml:"title,omitempty" json:"title,omitempty"`
	WebsiteURL string            `yaml:"websiteUrl,omitempty" json:"websiteUrl,omitempty"`
	Repository *AgentRepository  `yaml:"repository,omitempty" json:"repository,omitempty"`
	Packages   []AgentPackageRef `yaml:"packages,omitempty" json:"packages,omitempty"`
	Remotes    []AgentRemote     `yaml:"remotes,omitempty" json:"remotes,omitempty"`
}

// AgentMcpServer mirrors models.McpServerType.
type AgentMcpServer struct {
	Name                       string            `yaml:"name" json:"name"`
	Version                    string            `yaml:"version,omitempty" json:"version,omitempty"`
	Type                       string            `yaml:"type,omitempty" json:"type,omitempty"`
	Image                      string            `yaml:"image,omitempty" json:"image,omitempty"`
	Build                      string            `yaml:"build,omitempty" json:"build,omitempty"`
	Command                    string            `yaml:"command,omitempty" json:"command,omitempty"`
	Args                       []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env                        []string          `yaml:"env,omitempty" json:"env,omitempty"`
	URL                        string            `yaml:"url,omitempty" json:"url,omitempty"`
	Headers                    map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	RegistryURL                string            `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	RegistryServerName         string            `yaml:"registryServerName,omitempty" json:"registryServerName,omitempty"`
	RegistryServerVersion      string            `yaml:"registryServerVersion,omitempty" json:"registryServerVersion,omitempty"`
	RegistryServerPreferRemote bool              `yaml:"registryServerPreferRemote,omitempty" json:"registryServerPreferRemote,omitempty"`
}

// AgentSkillRef mirrors models.SkillRef.
type AgentSkillRef struct {
	Name                 string `yaml:"name" json:"name"`
	Image                string `yaml:"image,omitempty" json:"image,omitempty"`
	RegistryURL          string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	RegistrySkillName    string `yaml:"registrySkillName,omitempty" json:"registrySkillName,omitempty"`
	RegistrySkillVersion string `yaml:"registrySkillVersion,omitempty" json:"registrySkillVersion,omitempty"`
}

// AgentPromptRef mirrors models.PromptRef.
type AgentPromptRef struct {
	Name                  string `yaml:"name" json:"name"`
	RegistryURL           string `yaml:"registryURL,omitempty" json:"registryURL,omitempty"`
	RegistryPromptName    string `yaml:"registryPromptName,omitempty" json:"registryPromptName,omitempty"`
	RegistryPromptVersion string `yaml:"registryPromptVersion,omitempty" json:"registryPromptVersion,omitempty"`
}

// AgentRepository mirrors model.Repository.
type AgentRepository struct {
	URL       string `yaml:"url,omitempty" json:"url,omitempty"`
	Source    string `yaml:"source,omitempty" json:"source,omitempty"`
	ID        string `yaml:"id,omitempty" json:"id,omitempty"`
	Subfolder string `yaml:"subfolder,omitempty" json:"subfolder,omitempty"`
}

// AgentPackageRef mirrors models.AgentPackageInfo.
type AgentPackageRef struct {
	RegistryType string `yaml:"registryType" json:"registryType"`
	Identifier   string `yaml:"identifier" json:"identifier"`
	Version      string `yaml:"version" json:"version"`
	Transport    struct {
		Type string `yaml:"type" json:"type"`
	} `yaml:"transport" json:"transport"`
}

// AgentRemote mirrors model.Transport.
type AgentRemote struct {
	Type string `yaml:"type" json:"type"`
	URL  string `yaml:"url,omitempty" json:"url,omitempty"`
}

// ---------------------------------------------------------------------------
// Skill
// ---------------------------------------------------------------------------

// SkillSpec is the typed spec: block for a kind: skill declarative document.
// Fields mirror the SkillJSON wire contract so that typed decode is lossless.
type SkillSpec struct {
	Title       string            `yaml:"title,omitempty" json:"title,omitempty"`
	Category    string            `yaml:"category,omitempty" json:"category,omitempty"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	WebsiteURL  string            `yaml:"websiteUrl,omitempty" json:"websiteUrl,omitempty"`
	Repository  *SkillRepository  `yaml:"repository,omitempty" json:"repository,omitempty"`
	Packages    []SkillPackageRef `yaml:"packages,omitempty" json:"packages,omitempty"`
	Remotes     []SkillRemoteInfo `yaml:"remotes,omitempty" json:"remotes,omitempty"`
}

// SkillRepository mirrors models.SkillRepository.
type SkillRepository struct {
	URL    string `yaml:"url,omitempty" json:"url,omitempty"`
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
}

// SkillPackageRef mirrors models.SkillPackageInfo.
type SkillPackageRef struct {
	RegistryType string `yaml:"registryType" json:"registryType"`
	Identifier   string `yaml:"identifier" json:"identifier"`
	Version      string `yaml:"version" json:"version"`
	Transport    struct {
		Type string `yaml:"type" json:"type"`
	} `yaml:"transport" json:"transport"`
}

// SkillRemoteInfo mirrors models.SkillRemoteInfo.
type SkillRemoteInfo struct {
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
}

// ---------------------------------------------------------------------------
// Prompt
// ---------------------------------------------------------------------------

// PromptSpec is the typed spec: block for a kind: prompt declarative document.
// Fields mirror the PromptJSON wire contract so that typed decode is lossless.
type PromptSpec struct {
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Content     string `yaml:"content,omitempty" json:"content,omitempty"`
}

// ---------------------------------------------------------------------------
// MCP (server)
// ---------------------------------------------------------------------------

// MCPSpec is the typed spec: block for a kind: mcp declarative document.
// Fields mirror the ServerJSON wire contract so that typed decode is lossless.
type MCPSpec struct {
	Schema      string         `yaml:"$schema,omitempty" json:"$schema,omitempty"`
	Description string         `yaml:"description,omitempty" json:"description,omitempty"`
	Title       string         `yaml:"title,omitempty" json:"title,omitempty"`
	WebsiteURL  string         `yaml:"websiteUrl,omitempty" json:"websiteUrl,omitempty"`
	Repository  *MCPRepository `yaml:"repository,omitempty" json:"repository,omitempty"`
	Icons       []MCPIcon      `yaml:"icons,omitempty" json:"icons,omitempty"`
	Packages    []MCPPackage   `yaml:"packages,omitempty" json:"packages,omitempty"`
	Remotes     []MCPTransport `yaml:"remotes,omitempty" json:"remotes,omitempty"`
}

// MCPRepository mirrors model.Repository.
type MCPRepository struct {
	URL       string `yaml:"url,omitempty" json:"url,omitempty"`
	Source    string `yaml:"source,omitempty" json:"source,omitempty"`
	ID        string `yaml:"id,omitempty" json:"id,omitempty"`
	Subfolder string `yaml:"subfolder,omitempty" json:"subfolder,omitempty"`
}

// MCPIcon mirrors model.Icon.
type MCPIcon struct {
	Src      string   `yaml:"src" json:"src"`
	MimeType *string  `yaml:"mimeType,omitempty" json:"mimeType,omitempty"`
	Sizes    []string `yaml:"sizes,omitempty" json:"sizes,omitempty"`
	Theme    *string  `yaml:"theme,omitempty" json:"theme,omitempty"`
}

// MCPTransport mirrors model.Transport (used for both packages and remotes).
type MCPTransport struct {
	Type    string             `yaml:"type" json:"type"`
	URL     string             `yaml:"url,omitempty" json:"url,omitempty"`
	Headers []MCPKeyValueInput `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// MCPPackage mirrors model.Package.
type MCPPackage struct {
	RegistryType         string             `yaml:"registryType" json:"registryType"`
	RegistryBaseURL      string             `yaml:"registryBaseUrl,omitempty" json:"registryBaseUrl,omitempty"`
	Identifier           string             `yaml:"identifier" json:"identifier"`
	Version              string             `yaml:"version,omitempty" json:"version,omitempty"`
	FileSHA256           string             `yaml:"fileSha256,omitempty" json:"fileSha256,omitempty"`
	RunTimeHint          string             `yaml:"runtimeHint,omitempty" json:"runtimeHint,omitempty"`
	Transport            MCPTransport       `yaml:"transport" json:"transport"`
	RuntimeArguments     []MCPArgument      `yaml:"runtimeArguments,omitempty" json:"runtimeArguments,omitempty"`
	PackageArguments     []MCPArgument      `yaml:"packageArguments,omitempty" json:"packageArguments,omitempty"`
	EnvironmentVariables []MCPKeyValueInput `yaml:"environmentVariables,omitempty" json:"environmentVariables,omitempty"`
}

// MCPInputVariable mirrors model.Input — used as the value type in Variables maps.
type MCPInputVariable struct {
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	IsRequired  bool     `yaml:"isRequired,omitempty" json:"isRequired,omitempty"`
	Format      string   `yaml:"format,omitempty" json:"format,omitempty"`
	Value       string   `yaml:"value,omitempty" json:"value,omitempty"`
	IsSecret    bool     `yaml:"isSecret,omitempty" json:"isSecret,omitempty"`
	Default     string   `yaml:"default,omitempty" json:"default,omitempty"`
	Placeholder string   `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
	Choices     []string `yaml:"choices,omitempty" json:"choices,omitempty"`
}

// MCPArgument mirrors model.Argument.
type MCPArgument struct {
	Type        string                      `yaml:"type" json:"type"`
	Name        string                      `yaml:"name,omitempty" json:"name,omitempty"`
	ValueHint   string                      `yaml:"valueHint,omitempty" json:"valueHint,omitempty"`
	IsRepeated  bool                        `yaml:"isRepeated,omitempty" json:"isRepeated,omitempty"`
	Description string                      `yaml:"description,omitempty" json:"description,omitempty"`
	IsRequired  bool                        `yaml:"isRequired,omitempty" json:"isRequired,omitempty"`
	Format      string                      `yaml:"format,omitempty" json:"format,omitempty"`
	Value       string                      `yaml:"value,omitempty" json:"value,omitempty"`
	IsSecret    bool                        `yaml:"isSecret,omitempty" json:"isSecret,omitempty"`
	Default     string                      `yaml:"default,omitempty" json:"default,omitempty"`
	Placeholder string                      `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
	Choices     []string                    `yaml:"choices,omitempty" json:"choices,omitempty"`
	Variables   map[string]MCPInputVariable `yaml:"variables,omitempty" json:"variables,omitempty"`
}

// MCPKeyValueInput mirrors model.KeyValueInput (env vars, headers).
type MCPKeyValueInput struct {
	Name        string                      `yaml:"name" json:"name"`
	Description string                      `yaml:"description,omitempty" json:"description,omitempty"`
	IsRequired  bool                        `yaml:"isRequired,omitempty" json:"isRequired,omitempty"`
	Format      string                      `yaml:"format,omitempty" json:"format,omitempty"`
	Value       string                      `yaml:"value,omitempty" json:"value,omitempty"`
	IsSecret    bool                        `yaml:"isSecret,omitempty" json:"isSecret,omitempty"`
	Default     string                      `yaml:"default,omitempty" json:"default,omitempty"`
	Placeholder string                      `yaml:"placeholder,omitempty" json:"placeholder,omitempty"`
	Choices     []string                    `yaml:"choices,omitempty" json:"choices,omitempty"`
	Variables   map[string]MCPInputVariable `yaml:"variables,omitempty" json:"variables,omitempty"`
}

// ---------------------------------------------------------------------------
// Provider
// ---------------------------------------------------------------------------

// ProviderSpec is the typed spec: block for a kind: provider declarative document.
// Platform is the discriminator; Config is a generic map for platform-specific fields.
type ProviderSpec struct {
	Platform string         `yaml:"platform" json:"platform"`
	Config   map[string]any `yaml:"config,omitempty" json:"config,omitempty"`
}

// ---------------------------------------------------------------------------
// Deployment
// ---------------------------------------------------------------------------

// DeploymentSpec is the typed spec: block for a kind: deployment declarative document.
type DeploymentSpec struct {
	ProviderID     string            `yaml:"providerId" json:"providerId"`
	ResourceType   string            `yaml:"resourceType" json:"resourceType"` // "agent" | "mcp" (also accepts "server" as alias for "mcp")
	Env            map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	ProviderConfig models.JSONObject `yaml:"providerConfig,omitempty" json:"providerConfig,omitempty"`
	PreferRemote   bool              `yaml:"preferRemote,omitempty" json:"preferRemote,omitempty"`
}
