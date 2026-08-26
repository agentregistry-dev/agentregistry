package v1alpha1

// Agent is the typed envelope for kind=Agent resources.
type Agent struct {
	TypeMeta `json:",inline" yaml:",inline"`
	// metadata is part of Agent.
	// +required
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	// spec is part of Agent.
	// +required
	Spec AgentSpec `json:"spec" yaml:"spec"`
	// status is part of Agent.
	// +optional
	Status Status `json:"status,omitzero" yaml:"status,omitempty"`
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
	// title is the agent's catalog display name.
	// +optional
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// description is part of AgentSpec.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// iconUrl is the image a catalog UI shows for this agent. Either an
	// absolute https:// URL or a root-relative path served by the UI.
	// +optional
	IconURL string `json:"iconUrl,omitempty" yaml:"iconUrl,omitempty"`

	// modelProvider and ModelName are retained for one release so existing
	// Agent resources continue to decode and round-trip without data loss.
	//
	// Deprecated: these fields do not select or configure a runtime model.
	// New and migrated Deployments must use spec.modelRef; distributions may
	// temporarily preserve Deployment MODEL_PROVIDER / MODEL_NAME environment
	// values when modelRef is omitted.
	// +optional
	ModelProvider string `json:"modelProvider,omitempty" yaml:"modelProvider,omitempty" deprecated:"true"`
	// modelName is part of AgentSpec.
	// +optional
	ModelName string `json:"modelName,omitempty" yaml:"modelName,omitempty" deprecated:"true"`

	// source declares where the agent comes from — Image (the runtime
	// container) and/or Repository (the source code).
	// +optional
	Source *AgentSource `json:"source,omitempty" yaml:"source,omitempty"`

	// compatibleHarnesses declares which coding harnesses this Agent can run
	// under. The Deployment selects the concrete harness type for a
	// rollout; Agent remains the portable compatibility contract.
	// +optional
	CompatibleHarnesses []HarnessCompatibility `json:"compatibleHarnesses,omitempty" yaml:"compatibleHarnesses,omitempty"`

	// plugins are top-level, harness-agnostic references to what the agent
	// is assembled from. The selected Deployment harness materializes what it
	// supports and drops-with-warning the rest (capability matrix). Plugins,
	// Skills, and Instructions require compatibleHarnesses because a prebuilt
	// Image cannot consume them by itself. MCPServers flow to harness runtimes
	// and remain available to any other runtime that supports MCP. Each ref's
	// Kind defaults to the field's resource kind; empty Tag means "resolve
	// latest at reference time".
	// +optional
	Plugins []ResourceRef `json:"plugins,omitempty" yaml:"plugins,omitempty"`
	// skills is part of AgentSpec.
	// +optional
	Skills []ResourceRef `json:"skills,omitempty" yaml:"skills,omitempty"`
	// instructions is part of AgentSpec.
	// +optional
	Instructions *ResourceRef `json:"instructions,omitempty" yaml:"instructions,omitempty"`
	// mcpServers is part of AgentSpec.
	// +optional
	MCPServers []ResourceRef `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
}

// HasLegacyModelConfiguration reports whether an Agent still carries the
// one-release compatibility fields. The fields are intentionally
// non-authoritative; callers should use this only for warnings and migration
// inventory.
func (s AgentSpec) HasLegacyModelConfiguration() bool {
	return s.ModelProvider != "" || s.ModelName != ""
}

// AgentSource is the distribution origin of a bring-your-own container/source
// agent. Harness-based deployments select a compatible harness from
// AgentSpec.CompatibleHarnesses at Deployment time.
type AgentSource struct {
	// image is the OCI container image reference that runs the agent.
	// Format: <registry>/<name>:<tag> (e.g. ghcr.io/owner/agent:1.0.0).
	// +optional
	Image string `json:"image,omitempty" yaml:"image,omitempty"`

	// repository links to the source code the image was built from.
	// +optional
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`

	// protocol is the application protocol spoken by every runnable form of the
	// agent, whether built from Repository or supplied as Image. When omitted,
	// A2A is inferred as the default.
	// +optional
	Protocol *AgentProtocol `json:"protocol,omitempty" yaml:"protocol,omitempty" enum:"A2A,HTTP"`
}

// AgentProtocol is the application protocol exposed by an Agent source.
type AgentProtocol string

const (
	AgentProtocolA2A  AgentProtocol = "A2A"
	AgentProtocolHTTP AgentProtocol = "HTTP"
)

// HarnessCompatibility declares one harness family this Agent can run under.
// Rollout policy selection lives on Deployment so the same Agent can be rolled
// out with different compatible harnesses.
type HarnessCompatibility struct {
	// type is the harness family, e.g. "claude-code", "codex", "opencode".
	// +required
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type" yaml:"type"`
}
