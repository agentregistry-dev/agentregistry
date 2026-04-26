package v1alpha1

// Agent is the typed envelope for kind=Agent resources.
type Agent struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     AgentSpec  `json:"spec" yaml:"spec"`
	Status   Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

// AgentSpec is the agent resource's declarative body.
//
// References to other resources (MCP servers, skills, prompts) are pure
// ResourceRefs — no inline runtime configuration. To deploy an agent with a
// specific MCP server wired in, define a top-level MCPServer resource and
// reference it here.
type AgentSpec struct {
	// Core fields.
	Title             string  `json:"title,omitempty" yaml:"title,omitempty"`
	Description       string  `json:"description,omitempty" yaml:"description,omitempty"`
	Image             string  `json:"image,omitempty" yaml:"image,omitempty"`
	Language          string  `json:"language,omitempty" yaml:"language,omitempty"`
	Framework         string  `json:"framework,omitempty" yaml:"framework,omitempty"`
	ModelProvider     string  `json:"modelProvider,omitempty" yaml:"modelProvider,omitempty"`
	ModelName         string  `json:"modelName,omitempty" yaml:"modelName,omitempty"`
	TelemetryEndpoint string  `json:"telemetryEndpoint,omitempty" yaml:"telemetryEndpoint,omitempty"`
	WebsiteURL        string  `json:"websiteUrl,omitempty" yaml:"websiteUrl,omitempty"`
	Readme            *Readme `json:"readme,omitempty" yaml:"readme,omitempty"`

	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`

	// References to top-level resources. Each entry's Kind must match the
	// field name's singular form (MCPServer, Skill, Prompt). Version empty
	// means "resolve latest at reference time".
	MCPServers []ResourceRef `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
	Skills     []ResourceRef `json:"skills,omitempty" yaml:"skills,omitempty"`
	Prompts    []ResourceRef `json:"prompts,omitempty" yaml:"prompts,omitempty"`

	// Distribution metadata.
	Packages []AgentPackage `json:"packages,omitempty" yaml:"packages,omitempty"`
	Remotes  []AgentRemote  `json:"remotes,omitempty" yaml:"remotes,omitempty"`
}

// AgentPackage describes a distributable package of the agent (e.g. an OCI
// image or npm package reference).
type AgentPackage struct {
	RegistryType string         `json:"registryType" yaml:"registryType"`
	Identifier   string         `json:"identifier" yaml:"identifier"`
	Version      string         `json:"version" yaml:"version"`
	Transport    TransportProto `json:"transport" yaml:"transport"`
}

// AgentRemote describes a remote endpoint at which the agent is reachable.
type AgentRemote struct {
	Type string `json:"type" yaml:"type"`
	URL  string `json:"url,omitempty" yaml:"url,omitempty"`
}
