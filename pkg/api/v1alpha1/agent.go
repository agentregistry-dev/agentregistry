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

	// CompatibleHarnesses declares which coding harnesses this Agent can run
	// under. The Deployment selects the concrete harness type/version for a
	// rollout; Agent remains the portable compatibility contract.
	CompatibleHarnesses []HarnessCompatibility `json:"compatibleHarnesses,omitempty" yaml:"compatibleHarnesses,omitempty"`

	// Composition — top-level, harness-agnostic references to what the agent
	// is assembled from. The selected Deployment harness materializes what it
	// supports and drops-with-warning the rest (capability matrix). Plugins,
	// Skills, and Instructions require compatibleHarnesses because a prebuilt
	// Image cannot consume them by itself. MCPServers flow to harness runtimes
	// and remain available to any other runtime that supports MCP. Each ref's
	// Kind defaults to the field's resource kind; empty Tag means "resolve
	// latest at reference time".
	Plugins      []ResourceRef `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	Skills       []ResourceRef `json:"skills,omitempty" yaml:"skills,omitempty"`
	Instructions *ResourceRef  `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	MCPServers   []ResourceRef `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
}

// AgentSource is the distribution origin of a bring-your-own container/source
// agent. Harness-based deployments select a compatible harness from
// AgentSpec.CompatibleHarnesses at Deployment time.
type AgentSource struct {
	// Image is the OCI container image reference that runs the agent.
	// Format: <registry>/<name>:<tag> (e.g. ghcr.io/owner/agent:1.0.0).
	Image string `json:"image,omitempty" yaml:"image,omitempty"`

	// Repository links to the source code the image was built from.
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// HarnessCompatibility declares one harness family this Agent can run under.
// Rollout policy selection lives on Deployment so the same Agent can be rolled
// out with different compatible harnesses.
type HarnessCompatibility struct {
	// Type is the harness family, e.g. "claude-code", "codex", "opencode".
	Type string `json:"type" yaml:"type"`
}
