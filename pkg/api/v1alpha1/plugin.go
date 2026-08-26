package v1alpha1

// Plugin is the typed envelope for kind=Plugin resources.
//
// A Plugin is a self-contained, versioned bundle of harness extensions —
// skills, MCP servers, hooks, and sub-agents — modeled on the Claude Code
// plugin format. The Spec is USER INTENT ONLY: a pinned pointer to an external
// source (a git commit or — later — an OCI digest), the same source-based model
// agents and skills use. The registry hosts NOTHING; the Plugin controller
// resolves the pointer to a concrete commit/digest and scans the source for its
// manifest and inventory OUT OF BAND, recording that server-determined data in
// Status — never in Spec. The bundle is materialized from its source into a
// harness layout at deploy time.
type Plugin struct {
	TypeMeta `json:",inline" yaml:",inline"`
	// metadata is part of Plugin.
	// +required
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	// spec is part of Plugin.
	// +required
	Spec PluginSpec `json:"spec" yaml:"spec"`
	// status is part of Plugin.
	// +optional
	Status PluginStatus `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Plugin, PluginSpec](KindPlugin)
}

// PluginSpec is the plugin resource's declarative body — USER INTENT ONLY.
// Server-derived data (the resolved source pin, the parsed Manifest, and the
// derived Inventory) lives in PluginStatus, populated out of band by the Plugin
// controller. Keeping it out of the spec means a status write never changes the
// spec content hash, so re-applying identical intent is an UpsertNoOp.
type PluginSpec struct {
	// title is part of PluginSpec.
	// +optional
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// description is part of PluginSpec.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// iconUrl is the image a catalog UI shows for this plugin. Either an
	// absolute https:// URL or a root-relative path served by the UI.
	// +optional
	IconURL string `json:"iconUrl,omitempty" yaml:"iconUrl,omitempty"`

	// harnesses lists the harness formats this bundle carries native manifests
	// for (e.g. "claude-code", "codex"). It is informational in this phase;
	// deploy-time adapters decide which harnesses they can consume.
	// +optional
	Harnesses []string `json:"harnesses,omitempty" yaml:"harnesses,omitempty"`

	// source is where the bundle is ingested from, pinned (git commit / OCI
	// digest) so a published tag is reproducible.
	// +optional
	Source *PluginSource `json:"source,omitempty" yaml:"source,omitempty"`
}

// PluginStatus is the Plugin observed-state subresource, written by the Plugin
// controller out of band of the API write. It embeds the shared Status
// (conditions + observedGeneration) and adds the server-determined resolution
// data.
//
// Readiness contract: consumers MUST treat the absence of a Ready=True condition
// (or ResolvedSource==nil) as "not yet resolved". The controller sets
// Ready=False/Reason=Progressing on first observe, Ready=True/Reason=Resolved
// once the pointer is pinned and the source scanned, and Ready=False with a
// specific reason (SourceUnresolvable, SourceUnsupported, SourceInvalid) on
// failure.
type PluginStatus struct {
	Status `json:",inline" yaml:",inline"`

	// resolvedSource is the controller's immutable pin of the user's source
	// pointer (the concrete commit/digest the source resolved to).
	// +optional
	ResolvedSource *PluginResolvedSource `json:"resolvedSource,omitempty" yaml:"resolvedSource,omitempty"`
	// manifest is the canonical typed plugin.json parsed from the source.
	// +optional
	Manifest *PluginManifest `json:"manifest,omitempty" yaml:"manifest,omitempty"`
	// inventory is the server-derived risk surface / search index.
	// +optional
	Inventory *PluginInventory `json:"inventory,omitempty" yaml:"inventory,omitempty"`
}

// PluginResolvedSource records the concrete, immutable revision the controller
// pinned the user's source pointer to. Exactly one of Commit/Digest is set,
// matching Type. It is the reproducibility anchor: deploys materialize from this
// pin, not from the (possibly moving) ref the user supplied.
type PluginResolvedSource struct {
	// type is part of PluginResolvedSource.
	// +required
	Type PluginSourceType `json:"type" yaml:"type"`
	// commit is the resolved full git commit SHA (Type=git).
	// +optional
	Commit string `json:"commit,omitempty" yaml:"commit,omitempty"`
	// digest is the resolved OCI digest, e.g. "sha256:…" (Type=oci; future).
	// +optional
	Digest string `json:"digest,omitempty" yaml:"digest,omitempty"`
}

// PluginSourceType selects which source sub-struct is set.
type PluginSourceType string

const (
	PluginSourceTypeGit PluginSourceType = "git"
	PluginSourceTypeOCI PluginSourceType = "oci"
)

// PluginSource identifies where the bundle came from. Exactly one of Git/OCI
// is set, matching Type. The reference must be pinned (git commit / OCI digest)
// so the published tag is reproducible.
type PluginSource struct {
	// type is part of PluginSource.
	// +required
	Type PluginSourceType `json:"type" yaml:"type"`
	// git is part of PluginSource.
	// +optional
	Git *PluginSourceGit `json:"git,omitempty" yaml:"git,omitempty"`
	// oci is part of PluginSource.
	// +optional
	OCI *PluginSourceOCI `json:"oci,omitempty" yaml:"oci,omitempty"`
}

// PluginSourceGit is a git source. Repository may pin a Commit, a Branch, or a
// tag (empty => the remote default branch); the Plugin controller resolves
// whatever ref is supplied to a concrete commit SHA and records that immutable
// pin in status.ResolvedSource. Repository.Subfolder selects a plugin inside a
// monorepo.
type PluginSourceGit struct {
	// repository is part of PluginSourceGit.
	// +required
	Repository *Repository `json:"repository" yaml:"repository"`
}

// PluginSourceOCI is a digest-pinned OCI artifact reference, e.g.
// "ghcr.io/org/plugin@sha256:...". Bare/tag-only refs are rejected.
type PluginSourceOCI struct {
	// reference is part of PluginSourceOCI.
	// +required
	// +kubebuilder:validation:MinLength=1
	Reference string `json:"reference" yaml:"reference"`
}

// PluginInventory is the server-derived index of a bundle's actual contents,
// computed by scanning the bundle files (not the author-supplied manifest). It
// is the legible governance risk surface and the search index.
type PluginInventory struct {
	// skills is part of PluginInventory.
	// +optional
	Skills []PluginSkill `json:"skills,omitempty" yaml:"skills,omitempty"`
	// commands is part of PluginInventory.
	// +optional
	Commands []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	// agents are sub-agent names; sub-agents are markdown prompt files in the
	// bundle, not manifest entries.
	// +optional
	Agents []string `json:"agents,omitempty" yaml:"agents,omitempty"`
	// hooks are lifecycle hooks the bundle registers (arbitrary code).
	// +optional
	Hooks []PluginHook `json:"hooks,omitempty" yaml:"hooks,omitempty"`
	// mcpServers is part of PluginInventory.
	// +optional
	MCPServers []string `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
	// executables are bin/ entries the bundle ships (arbitrary code).
	// +optional
	Executables []string `json:"executables,omitempty" yaml:"executables,omitempty"`
}

// PluginSkill is one skill shipped in the bundle (from its SKILL.md frontmatter).
type PluginSkill struct {
	// name is part of PluginSkill.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
	// description is part of PluginSkill.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PluginHook is one lifecycle hook the bundle registers.
type PluginHook struct {
	// event is the lifecycle event, e.g. "PreToolUse", "PostToolUse",
	// "SessionStart".
	// +required
	// +kubebuilder:validation:MinLength=1
	Event string `json:"event" yaml:"event"`
	// type is the handler kind: command|http|mcp_tool|prompt|agent.
	// +optional
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
}
