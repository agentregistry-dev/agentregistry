package v1alpha1

// Plugin is the typed envelope for kind=Plugin resources.
//
// A Plugin is a self-contained, versioned bundle of harness extensions —
// skills, MCP servers, hooks, and sub-agents — modeled on the Claude Code
// plugin format. The registry stores a parsed canonical representation of the
// bundle (see PluginContent) and indexes its contents (see PluginManifest) for
// search, UI, and governance. Plugins are immutable by tag: the source is
// resolved and frozen at publish, so a given namespace/name/tag always
// materializes the same bytes. Translation into a specific harness's on-disk
// layout happens at pull/deploy time from the canonical form.
type Plugin struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     PluginSpec `json:"spec" yaml:"spec"`
	Status   Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Plugin, PluginSpec](KindPlugin)
}

// PluginSpec is the plugin resource's declarative body.
type PluginSpec struct {
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Harnesses lists the harness formats this bundle carries native manifests
	// for (e.g. "claude-code", "codex"). Informational: translation can target
	// other harnesses from the canonical form regardless of what's listed here.
	Harnesses []string `json:"harnesses,omitempty" yaml:"harnesses,omitempty"`

	// Origin is where the bundle was ingested from, pinned
	// (resolve-and-freeze at publish) so the tag stays immutable. Required on
	// publish; retained afterwards as provenance.
	Origin *PluginOrigin `json:"origin,omitempty" yaml:"origin,omitempty"`

	// Content addresses the canonical bundle the registry stored for this
	// plugin. Populated by the registry at publish time — not author-supplied.
	Content *PluginContent `json:"content,omitempty" yaml:"content,omitempty"`

	// Manifest is the canonical plugin manifest — a faithful, typed
	// representation of the bundle's .claude-plugin/plugin.json (the
	// lingua-franca format, a superset across harnesses). Parsed and populated
	// by the registry at publish.
	Manifest *PluginManifest `json:"manifest,omitempty" yaml:"manifest,omitempty"`

	// Inventory is the server-derived index of what the bundle actually ships
	// (skills, sub-agents, commands, hooks, MCP servers, bin/ executables),
	// computed by scanning the bundle files. It is the legible risk surface the
	// approval flow reviews (hooks + executables run arbitrary code) and powers
	// search. Distinct from Manifest because plugin.json may reference
	// components by path rather than inline. Populated by the registry at publish.
	Inventory *PluginInventory `json:"inventory,omitempty" yaml:"inventory,omitempty"`
}

// PluginOriginType selects which origin sub-struct is set.
type PluginOriginType string

const (
	PluginOriginTypeGit PluginOriginType = "git"
	PluginOriginTypeOCI PluginOriginType = "oci"
)

// PluginOrigin identifies where the bundle came from. Exactly one of Git/OCI
// is set, matching Type. The reference must be pinned (git commit / OCI digest)
// so the published tag is reproducible.
type PluginOrigin struct {
	Type PluginOriginType `json:"type" yaml:"type"`
	Git  *PluginOriginGit `json:"git,omitempty" yaml:"git,omitempty"`
	OCI  *PluginOriginOCI `json:"oci,omitempty" yaml:"oci,omitempty"`
}

// PluginOriginGit is a git source. Repository.Commit must be a full commit SHA
// so the origin is pinned; Repository.Subfolder selects a plugin inside a
// monorepo.
type PluginOriginGit struct {
	Repository *Repository `json:"repository" yaml:"repository"`
}

// PluginOriginOCI is a digest-pinned OCI artifact reference, e.g.
// "ghcr.io/org/plugin@sha256:...". Bare/tag-only refs are rejected.
type PluginOriginOCI struct {
	Reference string `json:"reference" yaml:"reference"`
}

// PluginContent addresses the canonical bundle stored by the registry. The
// canonical form is the portable core (SKILL.md, AGENTS.md, hooks, .mcp.json,
// sub-agent markdown) from which any target-harness layout is materialized.
type PluginContent struct {
	// ContentHash is the sha256 (hex) of the canonical bundle.
	ContentHash string `json:"contentHash,omitempty" yaml:"contentHash,omitempty"`
	// OCIRef is the OCI artifact reference where the registry stored the
	// canonical bundle (digest-pinned).
	OCIRef string `json:"ociRef,omitempty" yaml:"ociRef,omitempty"`
}

// PluginInventory is the server-derived index of a bundle's actual contents,
// computed by scanning the bundle files (not the author-supplied manifest). It
// is the legible governance risk surface and the search index.
type PluginInventory struct {
	Skills   []PluginSkill `json:"skills,omitempty" yaml:"skills,omitempty"`
	Commands []string      `json:"commands,omitempty" yaml:"commands,omitempty"`
	// Agents are sub-agent names; sub-agents are markdown prompt files in the
	// bundle, not manifest entries.
	Agents []string `json:"agents,omitempty" yaml:"agents,omitempty"`
	// Hooks are lifecycle hooks the bundle registers (arbitrary code).
	Hooks      []PluginHook `json:"hooks,omitempty" yaml:"hooks,omitempty"`
	MCPServers []string     `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
	// Executables are bin/ entries the bundle ships (arbitrary code).
	Executables []string `json:"executables,omitempty" yaml:"executables,omitempty"`
}

// PluginSkill is one skill shipped in the bundle (from its SKILL.md frontmatter).
type PluginSkill struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// PluginHook is one lifecycle hook the bundle registers.
type PluginHook struct {
	// Event is the lifecycle event, e.g. "PreToolUse", "PostToolUse",
	// "SessionStart".
	Event string `json:"event" yaml:"event"`
	// Type is the handler kind: command|http|mcp_tool|prompt|agent.
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
}
