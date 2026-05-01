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
	Title             string `json:"title,omitempty" yaml:"title,omitempty"`
	Description       string `json:"description,omitempty" yaml:"description,omitempty"`
	Language          string `json:"language,omitempty" yaml:"language,omitempty"`
	Framework         string `json:"framework,omitempty" yaml:"framework,omitempty"`
	ModelProvider     string `json:"modelProvider,omitempty" yaml:"modelProvider,omitempty"`
	ModelName         string `json:"modelName,omitempty" yaml:"modelName,omitempty"`
	TelemetryEndpoint string `json:"telemetryEndpoint,omitempty" yaml:"telemetryEndpoint,omitempty"`

	// Source declares where the agent comes from — Image (the runtime
	// container) and/or Repository (the source code).
	Source *AgentSource `json:"source,omitempty" yaml:"source,omitempty"`

	// References to top-level resources. Each entry's Kind must match the
	// field name's singular form (MCPServer, Skill, Prompt). Version empty
	// means "resolve latest at reference time".
	MCPServers []ResourceRef `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
	Skills     []ResourceRef `json:"skills,omitempty" yaml:"skills,omitempty"`
	Prompts    []ResourceRef `json:"prompts,omitempty" yaml:"prompts,omitempty"`
}

// AgentSource is the distribution origin of an agent — Image (the runtime
// container) and Repository (the source code).
type AgentSource struct {
	// Image is the OCI container image reference that runs the agent.
	// Format: <registry>/<name>:<tag> (e.g. ghcr.io/owner/agent:1.0.0).
	Image string `json:"image,omitempty" yaml:"image,omitempty"`

	// Repository links to the source code the image was built from.
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`
}
