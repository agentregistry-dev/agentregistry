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

	// Composition — top-level, harness-agnostic references to what the agent
	// is assembled from. The deployed harness materializes what it supports and
	// drops-with-warning the rest (capability matrix). Plugins, Skills, and
	// Instructions apply only to harness agents (Source.Harness); a prebuilt
	// Image cannot consume them. MCPServers flow to harness runtimes and remain
	// available to any other runtime that supports MCP. Each ref's Kind defaults
	// to the field's resource kind; empty Tag means "resolve latest at
	// reference time".
	Plugins      []ResourceRef `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Skills       []ResourceRef `json:"skills,omitempty" yaml:"skills,omitempty"`
	Instructions *ResourceRef  `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	MCPServers   []ResourceRef `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
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
	// OpenCode, ...) assembled from the agent's top-level composition refs
	// (plugins, skills, instructions, mcpServers) rather than a prebuilt
	// container. Mutually exclusive with Image.
	Harness *HarnessConfig `json:"harness,omitempty" yaml:"harness,omitempty"`
}

// HarnessConfig declares a harness-based agent: which coding harness to run.
// What to compose into it (plugins, skills, instructions, MCP servers) lives on
// AgentSpec as top-level, harness-agnostic refs; the harness adapter maps what
// it supports at deploy time. Model routing reuses AgentSpec.ModelProvider /
// ModelName.
type HarnessConfig struct {
	// Type is the harness to run, e.g. "claude-code", "codex", "opencode".
	Type string `json:"type" yaml:"type"`

	// Version pins the harness version (optional; latest if empty).
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}
