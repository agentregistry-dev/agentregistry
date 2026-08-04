// Package pluginmarketplace provides a read-only translation of
// AgentRegistry's Plugin resources into a Claude Code `marketplace.json`
// document (code.claude.com/docs/en/plugin-marketplaces).
//
// Only the bare-URL/git installation path is targeted (phase 1): the schema
// here covers the "url" and "git-subdir" source forms, which is what Claude
// Code needs to install a plugin from a resolved git commit pin. It does not
// cover the "github" source form (owner/repo shorthand) since AgentRegistry
// always has a concrete repository URL to hand.
//
// The translation is one-directional (v1alpha1.Plugin -> marketplace.json)
// and read-only; there is no publish/write path here.
package pluginmarketplace

// SchemaURL is the `$schema` value emitted on every MarketplaceResponse.
const SchemaURL = "https://json.schemastore.org/claude-code-marketplace.json"

// MarketplaceResponse is the top-level `marketplace.json` document.
type MarketplaceResponse struct {
	Schema  string        `json:"$schema,omitempty"`
	Name    string        `json:"name"`
	Owner   Owner         `json:"owner"`
	// Plugins is never emitted as null: Claude Code's marketplace.json parser
	// rejects a null plugins field outright, so an empty catalogue must still
	// marshal as `[]`. The nullable:"false" tag keeps the generated OpenAPI
	// schema from allowing null, matching what the handler actually emits.
	Plugins []PluginEntry `json:"plugins" nullable:"false"`
}

// Owner identifies who publishes the marketplace.
type Owner struct {
	Name  string `json:"name"`
	Email string `json:"email,omitempty"`
}

// PluginEntry is one installable plugin in the marketplace. Source is one of
// URLSource or GitSubdirSource, matching how the Plugin's resolved source pin
// was shaped.
type PluginEntry struct {
	Name        string `json:"name"`
	Source      any    `json:"source"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
}

// URLSource points at a plugin whose repository root is the plugin itself.
type URLSource struct {
	Source string `json:"source"` // "url"
	URL    string `json:"url"`
	SHA    string `json:"sha,omitempty"`
}

// GitSubdirSource points at a plugin living in a subfolder of a monorepo.
type GitSubdirSource struct {
	Source string `json:"source"` // "git-subdir"
	URL    string `json:"url"`
	Path   string `json:"path"`
	SHA    string `json:"sha,omitempty"`
}
