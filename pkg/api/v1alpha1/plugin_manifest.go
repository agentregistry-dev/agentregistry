package v1alpha1

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// PluginManifest is a faithful, lossless Go representation of a Claude Code
// plugin manifest (`.claude-plugin/plugin.json`). The registry records it as
// server-derived Plugin status after resolving and scanning the configured
// source; it does not make the manifest or bundle bytes part of the user-owned
// Plugin spec. It is grounded in the official schema
// (json.schemastore.org/claude-code-plugin-manifest.json).
//
// Fidelity rules:
//   - Every field maps to the real plugin.json key with an exact json tag.
//   - Optional scalars/objects use pointers or omitempty so a sparse manifest
//     round-trips to the same sparse JSON (no zero-value injection).
//   - Fields whose JSON is a `string | array | object` union use the custom
//     union types in this file, which preserve the source's exact form.
//   - Foreign-ecosystem and forward-compat top-level keys (e.g. Codex
//     `interface`, `apps`) land in Extras, making this a true cross-harness
//     superset rather than relying on lenient "ignore unknown fields" behavior.
//
// Scope notes: the array forms of hooks/mcpServers/lspServers are preserved
// verbatim (Raw) for lossless round-trip; the legible risk surface for those is
// the server-derived PluginInventory (which scans the actual bundle files), not
// this author-supplied manifest. Unknown keys inside the open object forms of
// dependencies/commands/monitors are not separately preserved.
//
// This type is NOT a registry kind; it is parsed from a plugin bundle and
// embedded in Plugin status (see plugin.go).
type PluginManifest struct {
	// $schema is part of PluginManifest.
	// +optional
	Schema string `json:"$schema,omitempty" yaml:"$schema,omitempty"`
	// name is part of PluginManifest.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
	// version is part of PluginManifest.
	// +optional
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
	// description is part of PluginManifest.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// homepage is part of PluginManifest.
	// +optional
	Homepage string `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	// repository is part of PluginManifest.
	// +optional
	Repository string `json:"repository,omitempty" yaml:"repository,omitempty"`
	// license is part of PluginManifest.
	// +optional
	License string `json:"license,omitempty" yaml:"license,omitempty"`
	// keywords is part of PluginManifest.
	// +optional
	Keywords []string `json:"keywords,omitempty" yaml:"keywords,omitempty"`

	// author is part of PluginManifest.
	// +optional
	Author *PluginAuthor `json:"author,omitempty" yaml:"author,omitempty"`

	// settings is an opaque allowlisted settings-merge object (schema models it
	// as open additionalProperties), held raw to round-trip losslessly.
	// +optional
	Settings json.RawMessage `json:"settings,omitempty" yaml:"settings,omitempty"`

	// dependencies is part of PluginManifest.
	// +optional
	Dependencies []PluginDependency `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`

	// commands configures component path overrides as a string, array, or object.
	// +optional
	Commands *CommandsField `json:"commands,omitempty" yaml:"commands,omitempty"`
	// agents is part of PluginManifest.
	// +optional
	Agents *PathOrPaths `json:"agents,omitempty" yaml:"agents,omitempty"`
	// skills is part of PluginManifest.
	// +optional
	Skills *PathOrPaths `json:"skills,omitempty" yaml:"skills,omitempty"`
	// outputStyles is part of PluginManifest.
	// +optional
	OutputStyles *PathOrPaths `json:"outputStyles,omitempty" yaml:"outputStyles,omitempty"`
	// hooks is part of PluginManifest.
	// +optional
	Hooks *HooksField `json:"hooks,omitempty" yaml:"hooks,omitempty"`
	// mcpServers is part of PluginManifest.
	// +optional
	MCPServers *MCPServersField `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
	// lspServers is part of PluginManifest.
	// +optional
	LSPServers *LSPServersField `json:"lspServers,omitempty" yaml:"lspServers,omitempty"`

	// userConfig is part of PluginManifest.
	// +optional
	UserConfig map[string]PluginUserConfigField `json:"userConfig,omitempty" yaml:"userConfig,omitempty"`
	// channels is part of PluginManifest.
	// +optional
	Channels []PluginChannel `json:"channels,omitempty" yaml:"channels,omitempty"`

	// themes use the schemastore top-level placement; experimental is
	// the docs-preferred nesting. Both are modeled so we re-emit whichever the
	// source used.
	// +optional
	Themes *PathOrPaths `json:"themes,omitempty" yaml:"themes,omitempty"`
	// monitors is part of PluginManifest.
	// +optional
	Monitors *MonitorsField `json:"monitors,omitempty" yaml:"monitors,omitempty"`
	// experimental is part of PluginManifest.
	// +optional
	Experimental *PluginExperimental `json:"experimental,omitempty" yaml:"experimental,omitempty"`

	// displayName / DefaultEnabled are docs-only (not in the schemastore schema)
	// but Claude loads them; modeled so real data isn't dropped.
	// +optional
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	// defaultEnabled is part of PluginManifest.
	// +optional
	DefaultEnabled *bool `json:"defaultEnabled,omitempty" yaml:"defaultEnabled,omitempty"`

	// Extras captures any top-level key not modeled above (Codex interface/apps,
	// forward-compat keys) so the manifest is a true cross-harness superset.
	// Spliced in/out by (Un)MarshalJSON; never carries a known key.
	Extras map[string]json.RawMessage `json:"-" yaml:"-"`
}

type pluginManifestWire PluginManifest

var knownManifestKeys = map[string]struct{}{
	"$schema": {}, "name": {}, "version": {}, "description": {}, "homepage": {},
	"repository": {}, "license": {}, "keywords": {}, "author": {}, "settings": {},
	"dependencies": {}, "commands": {}, "agents": {}, "skills": {},
	"outputStyles": {}, "hooks": {}, "mcpServers": {}, "lspServers": {},
	"userConfig": {}, "channels": {}, "themes": {}, "monitors": {},
	"experimental": {}, "displayName": {}, "defaultEnabled": {},
}

// UnmarshalJSON decodes the modeled fields and stashes every other top-level key
// in Extras, so no source data is lost on round-trip.
func (m *PluginManifest) UnmarshalJSON(data []byte) error {
	var w pluginManifestWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return err
	}
	for k, v := range all {
		if _, known := knownManifestKeys[k]; known {
			continue
		}
		if w.Extras == nil {
			w.Extras = map[string]json.RawMessage{}
		}
		w.Extras[k] = v
	}
	*m = PluginManifest(w)
	return nil
}

// MarshalJSON emits the modeled fields plus any Extras keys, re-merged at the
// top level. Modeled keys win on collision (Extras should never hold one).
func (m PluginManifest) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(pluginManifestWire(m))
	if err != nil {
		return nil, err
	}
	if len(m.Extras) == 0 {
		return base, nil
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	for k, v := range m.Extras {
		if _, known := knownManifestKeys[k]; known {
			continue
		}
		merged[k] = v
	}
	return json.Marshal(merged)
}

// PluginAuthor is the `author` block; Name is required when the block exists.
type PluginAuthor struct {
	// name is part of PluginAuthor.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
	// email is part of PluginAuthor.
	// +optional
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
	// url is part of PluginAuthor.
	// +optional
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
}

// PluginDependency is one `dependencies[]` entry: a string spec ("name",
// "name@marketplace", "name@^1.2.3") OR an object {name, marketplace, version}.
// Exactly one form is populated and preserved by (Un)MarshalJSON.
type PluginDependency struct {
	Ref string `json:"-" yaml:"-"`
	// name is part of PluginDependency.
	// +optional
	Name string `json:"name,omitempty" yaml:"name,omitempty"`
	// marketplace is part of PluginDependency.
	// +optional
	Marketplace string `json:"marketplace,omitempty" yaml:"marketplace,omitempty"`
	// version is part of PluginDependency.
	// +optional
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

func (d PluginDependency) MarshalJSON() ([]byte, error) {
	if d.Ref != "" {
		return json.Marshal(d.Ref)
	}
	type alias PluginDependency
	return json.Marshal(alias(d))
}

func (d *PluginDependency) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) > 0 && t[0] == '"' {
		return json.Unmarshal(t, &d.Ref)
	}
	type alias PluginDependency
	var a alias
	if err := json.Unmarshal(t, &a); err != nil {
		return err
	}
	*d = PluginDependency(a)
	return nil
}

// PathOrPaths models a `string | array<string>` component-path override. It
// normalizes to []string but remembers whether the source was a scalar so it
// re-emits the original form.
type PathOrPaths struct {
	// +optional
	Values []string
	// +optional
	WasArray bool
}

func (p PathOrPaths) MarshalJSON() ([]byte, error) {
	if !p.WasArray && len(p.Values) == 1 {
		return json.Marshal(p.Values[0])
	}
	return json.Marshal(p.Values)
}

func (p *PathOrPaths) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || string(t) == "null" {
		return nil
	}
	if t[0] == '[' {
		p.WasArray = true
		return json.Unmarshal(t, &p.Values)
	}
	var s string
	if err := json.Unmarshal(t, &s); err != nil {
		return err
	}
	p.Values = []string{s}
	return nil
}

// CommandsField models `commands`: paths (string|array) and/or an object map of
// named command entries.
type CommandsField struct {
	// +optional
	Paths *PathOrPaths
	// +optional
	Map map[string]CommandEntry
}

// CommandEntry is one named command in the object form.
type CommandEntry struct {
	// source is part of CommandEntry.
	// +optional
	Source string `json:"source,omitempty" yaml:"source,omitempty"`
	// content is part of CommandEntry.
	// +optional
	Content string `json:"content,omitempty" yaml:"content,omitempty"`
	// description is part of CommandEntry.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	// argumentHint is part of CommandEntry.
	// +optional
	ArgumentHint string `json:"argumentHint,omitempty" yaml:"argumentHint,omitempty"`
	// model is part of CommandEntry.
	// +optional
	Model string `json:"model,omitempty" yaml:"model,omitempty"`
	// allowedTools is part of CommandEntry.
	// +optional
	AllowedTools []string `json:"allowedTools,omitempty" yaml:"allowedTools,omitempty"`
}

func (c CommandsField) MarshalJSON() ([]byte, error) {
	if c.Map != nil {
		return json.Marshal(c.Map)
	}
	if c.Paths != nil {
		return c.Paths.MarshalJSON()
	}
	return []byte("null"), nil
}

func (c *CommandsField) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || string(t) == "null" {
		return nil
	}
	if t[0] == '{' {
		return json.Unmarshal(t, &c.Map)
	}
	var p PathOrPaths
	if err := p.UnmarshalJSON(t); err != nil {
		return err
	}
	c.Paths = &p
	return nil
}

// HooksField models `hooks`: a `./*.json` path (Path), an inline event->matcher
// object (Events), or an array form (kept Raw for lossless round-trip; read the
// derived PluginInventory for the array form's risk surface).
type HooksField struct {
	// +optional
	Path string
	// +optional
	Events map[string][]HookMatcherGroup
	// +optional
	Raw json.RawMessage
}

func (h HooksField) MarshalJSON() ([]byte, error) {
	switch {
	case h.Events != nil:
		return json.Marshal(h.Events)
	case h.Path != "":
		return json.Marshal(h.Path)
	case len(h.Raw) > 0:
		return h.Raw, nil
	default:
		return []byte("null"), nil
	}
}

func (h *HooksField) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || string(t) == "null" {
		return nil
	}
	switch t[0] {
	case '"':
		return json.Unmarshal(t, &h.Path)
	case '{':
		return json.Unmarshal(t, &h.Events)
	case '[':
		h.Raw = append(h.Raw[:0], t...)
		return nil
	default:
		return fmt.Errorf("v1alpha1: invalid hooks value %q", string(t))
	}
}

// HookMatcherGroup is one matcher group under an event.
type HookMatcherGroup struct {
	// matcher is part of HookMatcherGroup.
	// +optional
	Matcher string `json:"matcher,omitempty" yaml:"matcher,omitempty"`
	// hooks is part of HookMatcherGroup.
	// +required
	Hooks []HookEntry `json:"hooks" yaml:"hooks"`
}

// HookEntry is one hook action discriminated by Type (command|prompt|agent|
// http|mcp_tool). Variant fields are flattened with omitempty; per-type
// required/forbidden sets are enforced in plugin_validate.go.
type HookEntry struct {
	// type is part of HookEntry.
	// +required
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type" yaml:"type"`

	// command is part of HookEntry.
	// +optional
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// shell is part of HookEntry.
	// +optional
	Shell string `json:"shell,omitempty" yaml:"shell,omitempty"`

	// prompt is part of HookEntry.
	// +optional
	Prompt string `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	// model is part of HookEntry.
	// +optional
	Model string `json:"model,omitempty" yaml:"model,omitempty"`

	// url is part of HookEntry.
	// +optional
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// headers is part of HookEntry.
	// +optional
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	// allowedEnvVars is part of HookEntry.
	// +optional
	AllowedEnvVars []string `json:"allowedEnvVars,omitempty" yaml:"allowedEnvVars,omitempty"`

	// server is part of HookEntry.
	// +optional
	Server string `json:"server,omitempty" yaml:"server,omitempty"`
	// tool is part of HookEntry.
	// +optional
	Tool string `json:"tool,omitempty" yaml:"tool,omitempty"`
	// input is part of HookEntry.
	// +optional
	Input json.RawMessage `json:"input,omitempty" yaml:"input,omitempty"`

	// if is part of HookEntry.
	// +optional
	If string `json:"if,omitempty" yaml:"if,omitempty"`
	// timeout is a pointer so an explicit "timeout": 0 (disable) round-trips
	// losslessly — a float64 with omitempty would silently drop the zero.
	// +optional
	Timeout *float64 `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// statusMessage is part of HookEntry.
	// +optional
	StatusMessage string `json:"statusMessage,omitempty" yaml:"statusMessage,omitempty"`
	// once is part of HookEntry.
	// +optional
	Once *bool `json:"once,omitempty" yaml:"once,omitempty"`
	// async is part of HookEntry.
	// +optional
	Async *bool `json:"async,omitempty" yaml:"async,omitempty"`
	// asyncRewake is part of HookEntry.
	// +optional
	AsyncRewake *bool `json:"asyncRewake,omitempty" yaml:"asyncRewake,omitempty"`
}

// MCPServersField models `mcpServers`: a path/MCPB string (Path), an inline
// name->config object (Servers), or an array form (Raw).
type MCPServersField struct {
	// +optional
	Path string
	// +optional
	Servers map[string]MCPServerEntry
	// +optional
	Raw json.RawMessage
}

func (f MCPServersField) MarshalJSON() ([]byte, error) {
	switch {
	case f.Servers != nil:
		return json.Marshal(f.Servers)
	case f.Path != "":
		return json.Marshal(f.Path)
	case len(f.Raw) > 0:
		return f.Raw, nil
	default:
		return []byte("null"), nil
	}
}

func (f *MCPServersField) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || string(t) == "null" {
		return nil
	}
	switch t[0] {
	case '"':
		return json.Unmarshal(t, &f.Path)
	case '{':
		return json.Unmarshal(t, &f.Servers)
	case '[':
		f.Raw = append(f.Raw[:0], t...)
		return nil
	default:
		return fmt.Errorf("v1alpha1: invalid mcpServers value %q", string(t))
	}
}

// MCPServerEntry is one inline MCP server config (stdio|sse|http|ws).
type MCPServerEntry struct {
	// type is part of MCPServerEntry.
	// +optional
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// command is part of MCPServerEntry.
	// +optional
	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	// args is part of MCPServerEntry.
	// +optional
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`
	// env is part of MCPServerEntry.
	// +optional
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty"`

	// url is part of MCPServerEntry.
	// +optional
	URL string `json:"url,omitempty" yaml:"url,omitempty"`
	// headers is part of MCPServerEntry.
	// +optional
	Headers map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	// headersHelper is part of MCPServerEntry.
	// +optional
	HeadersHelper string `json:"headersHelper,omitempty" yaml:"headersHelper,omitempty"`
	// oauth is part of MCPServerEntry.
	// +optional
	OAuth *MCPServerOAuth `json:"oauth,omitempty" yaml:"oauth,omitempty"`
}

// MCPServerOAuth is the sse/http oauth sub-block.
type MCPServerOAuth struct {
	// clientId is part of MCPServerOAuth.
	// +optional
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty"`
	// callbackPort preserves an explicit zero value when it round-trips.
	// +optional
	CallbackPort *int `json:"callbackPort,omitempty" yaml:"callbackPort,omitempty"`
	// authServerMetadataUrl is part of MCPServerOAuth.
	// +optional
	AuthServerMetadataURL string `json:"authServerMetadataUrl,omitempty" yaml:"authServerMetadataUrl,omitempty"`
	// scopes is part of MCPServerOAuth.
	// +optional
	Scopes []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// LSPServersField models `lspServers`: a path string, an inline name->config
// object, or an array form (Raw).
type LSPServersField struct {
	// +optional
	Path string
	// +optional
	Servers map[string]LSPServerEntry
	// +optional
	Raw json.RawMessage
}

func (f LSPServersField) MarshalJSON() ([]byte, error) {
	switch {
	case f.Servers != nil:
		return json.Marshal(f.Servers)
	case f.Path != "":
		return json.Marshal(f.Path)
	case len(f.Raw) > 0:
		return f.Raw, nil
	default:
		return []byte("null"), nil
	}
}

func (f *LSPServersField) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || string(t) == "null" {
		return nil
	}
	switch t[0] {
	case '"':
		return json.Unmarshal(t, &f.Path)
	case '{':
		return json.Unmarshal(t, &f.Servers)
	case '[':
		f.Raw = append(f.Raw[:0], t...)
		return nil
	default:
		return fmt.Errorf("v1alpha1: invalid lspServers value %q", string(t))
	}
}

// LSPServerEntry is one inline LSP server config.
type LSPServerEntry struct {
	// command is part of LSPServerEntry.
	// +required
	// +kubebuilder:validation:MinLength=1
	Command string `json:"command" yaml:"command"`
	// args is part of LSPServerEntry.
	// +optional
	Args []string `json:"args,omitempty" yaml:"args,omitempty"`
	// extensionToLanguage is part of LSPServerEntry.
	// +required
	ExtensionToLanguage map[string]string `json:"extensionToLanguage" yaml:"extensionToLanguage"`
	// transport is part of LSPServerEntry.
	// +optional
	Transport string `json:"transport,omitempty" yaml:"transport,omitempty"`
	// env is part of LSPServerEntry.
	// +optional
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	// initializationOptions is part of LSPServerEntry.
	// +optional
	InitializationOptions json.RawMessage `json:"initializationOptions,omitempty" yaml:"initializationOptions,omitempty"`
	// settings is part of LSPServerEntry.
	// +optional
	Settings json.RawMessage `json:"settings,omitempty" yaml:"settings,omitempty"`
	// workspaceFolder is part of LSPServerEntry.
	// +optional
	WorkspaceFolder string `json:"workspaceFolder,omitempty" yaml:"workspaceFolder,omitempty"`
	// startupTimeout is part of LSPServerEntry.
	// +optional
	StartupTimeout *int `json:"startupTimeout,omitempty" yaml:"startupTimeout,omitempty"`
	// maxRestarts is part of LSPServerEntry.
	// +optional
	MaxRestarts *int `json:"maxRestarts,omitempty" yaml:"maxRestarts,omitempty"`
}

// PluginUserConfigField is one typed enable-time prompt. Default is a
// string|number|boolean|string[] union held raw.
type PluginUserConfigField struct {
	// type is part of PluginUserConfigField.
	// +required
	// +kubebuilder:validation:MinLength=1
	Type string `json:"type" yaml:"type"`
	// title is part of PluginUserConfigField.
	// +required
	// +kubebuilder:validation:MinLength=1
	Title string `json:"title" yaml:"title"`
	// description is part of PluginUserConfigField.
	// +required
	// +kubebuilder:validation:MinLength=1
	Description string `json:"description" yaml:"description"`
	// required is part of PluginUserConfigField.
	// +optional
	Required *bool `json:"required,omitempty" yaml:"required,omitempty"`
	// default is part of PluginUserConfigField.
	// +optional
	Default json.RawMessage `json:"default,omitempty" yaml:"default,omitempty"`
	// multiple is part of PluginUserConfigField.
	// +optional
	Multiple *bool `json:"multiple,omitempty" yaml:"multiple,omitempty"`
	// sensitive is part of PluginUserConfigField.
	// +optional
	Sensitive *bool `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
	// min is part of PluginUserConfigField.
	// +optional
	Min *float64 `json:"min,omitempty" yaml:"min,omitempty"`
	// max is part of PluginUserConfigField.
	// +optional
	Max *float64 `json:"max,omitempty" yaml:"max,omitempty"`
}

// PluginChannel declares an MCP-server-backed message channel.
type PluginChannel struct {
	// server is part of PluginChannel.
	// +required
	// +kubebuilder:validation:MinLength=1
	Server string `json:"server" yaml:"server"`
	// displayName is part of PluginChannel.
	// +optional
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	// userConfig is part of PluginChannel.
	// +optional
	UserConfig map[string]PluginUserConfigField `json:"userConfig,omitempty" yaml:"userConfig,omitempty"`
}

// MonitorsField models `monitors`: a `./*.json` path or an array of monitors.
type MonitorsField struct {
	// +optional
	Path string
	// +optional
	Entries []MonitorEntry
}

func (f MonitorsField) MarshalJSON() ([]byte, error) {
	if f.Entries != nil {
		return json.Marshal(f.Entries)
	}
	if f.Path != "" {
		return json.Marshal(f.Path)
	}
	return []byte("null"), nil
}

func (f *MonitorsField) UnmarshalJSON(data []byte) error {
	t := bytes.TrimSpace(data)
	if len(t) == 0 || string(t) == "null" {
		return nil
	}
	if t[0] == '[' {
		return json.Unmarshal(t, &f.Entries)
	}
	return json.Unmarshal(t, &f.Path)
}

// MonitorEntry is one inline monitor.
type MonitorEntry struct {
	// name is part of MonitorEntry.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
	// command is part of MonitorEntry.
	// +required
	// +kubebuilder:validation:MinLength=1
	Command string `json:"command" yaml:"command"`
	// description is part of MonitorEntry.
	// +required
	// +kubebuilder:validation:MinLength=1
	Description string `json:"description" yaml:"description"`
	// when is part of MonitorEntry.
	// +optional
	When string `json:"when,omitempty" yaml:"when,omitempty"`
}

// PluginExperimental is the docs-preferred nesting for themes/monitors. Typed
// (not raw) so the derived inventory/governance can read it; unknown
// experimental keys are not separately preserved.
type PluginExperimental struct {
	// themes is part of PluginExperimental.
	// +optional
	Themes *PathOrPaths `json:"themes,omitempty" yaml:"themes,omitempty"`
	// monitors is part of PluginExperimental.
	// +optional
	Monitors *MonitorsField `json:"monitors,omitempty" yaml:"monitors,omitempty"`
}
