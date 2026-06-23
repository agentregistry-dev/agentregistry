package v1alpha1

// Agent is the typed envelope for kind=Agent resources.
type Agent struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     AgentSpec  `json:"spec" yaml:"spec"`
	Status   Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Agent, AgentSpec](KindAgent)
}

// AgentSpec is the agent resource's declarative body.
//
// References to other resources (MCP servers) are pure ResourceRefs — no
// inline runtime configuration. To deploy an agent with a specific MCP server
// wired in, define a top-level MCPServer resource and reference it here.
type AgentSpec struct {
	// Core fields.
	Title         string `json:"title,omitempty" yaml:"title,omitempty"`
	Description   string `json:"description,omitempty" yaml:"description,omitempty"`
	ModelProvider string `json:"modelProvider,omitempty" yaml:"modelProvider,omitempty"`
	ModelName     string `json:"modelName,omitempty" yaml:"modelName,omitempty"`

	// Source declares where the agent comes from — Image (the runtime
	// container) and/or Repository (the source code).
	Source *AgentSource `json:"source,omitempty" yaml:"source,omitempty"`

	// References to top-level resources. Each entry's Kind must match the
	// field name's singular form (MCPServer). Tag empty means
	// "resolve latest at reference time".
	//
	// Skills + Prompts removed per audit; refs were ADK-Python-runtime-specific
	// and not generalizable.
	MCPServers []ResourceRef `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
}

// AgentSource is the distribution origin of an agent. A harness-based agent
// (Harness) is the first-class model: a coding harness run from declarative
// config. Image + Repository remain supported for bring-your-own
// container/source agents. Harness is mutually exclusive with Image.
type AgentSource struct {
	// Image is the OCI container image reference that runs the agent.
	// Format: <registry>/<name>:<tag> (e.g. ghcr.io/owner/agent:1.0.0).
	Image string `json:"image,omitempty" yaml:"image,omitempty"`

	// Repository links to the source code the image was built from.
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`

	// Harness declares a harness-based agent (the first-class deployment
	// model). When set, the agent runs a coding harness (Claude Code, Codex,
	// OpenCode, ...) materialized from the referenced plugins and optional
	// instructions rather than a prebuilt container. Mutually exclusive with
	// Image. MCP wiring stays on AgentSpec.MCPServers.
	Harness *HarnessConfig `json:"harness,omitempty" yaml:"harness,omitempty"`
}

// HarnessConfig declares a harness-based agent. At deploy time the registry
// materializes the referenced plugins into the on-disk layout the harness Type
// expects, applies optional instructions, and runs the harness behind the target
// platform's invocation contract. Model routing reuses AgentSpec.ModelProvider /
// ModelName, and MCP wiring reuses AgentSpec.MCPServers.
type HarnessConfig struct {
	// Type is the harness to run, e.g. "claude-code", "codex", "opencode".
	Type string `json:"type" yaml:"type"`

	// Version pins the harness version (optional; latest if empty).
	Version string `json:"version,omitempty" yaml:"version,omitempty"`

	// Plugins are materialized into the harness filesystem at deploy time.
	Plugins []ResourceRef `json:"plugins,omitempty" yaml:"plugins,omitempty"`

	// Instructions references a Prompt providing the system prompt / AGENTS.md.
	Instructions *ResourceRef `json:"instructions,omitempty" yaml:"instructions,omitempty"`
}
