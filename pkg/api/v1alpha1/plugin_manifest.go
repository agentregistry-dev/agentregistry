package v1alpha1

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// PluginManifest is a faithful, lossless Go representation of a Claude Code
// plugin manifest (`.claude-plugin/plugin.json`) — the canonical lingua-franca
// format AgentRegistry stores and translates between harnesses. It is grounded
// in the official schema (json.schemastore.org/claude-code-plugin-manifest.json).
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
// This type is NOT a registry kind; it is the canonical content parsed from a
// plugin bundle and embedded in a Plugin resource (see plugin.go).
type PluginManifest struct {
	Schema      string   `json:"$schema,omitempty" yaml:"$schema,omitempty"`
	Name        string   `json:"name" yaml:"name"`
	Version     string   `json:"version,omitempty" yaml:"version,omitempty"`
	Description string   `json:"description,omitempty" yaml:"description,omitempty"`
	Homepage    string   `json:"homepage,omitempty" yaml:"homepage,omitempty"`
	Repository  string   `json:"repository,omitempty" yaml:"repository,omitempty"`
	License     string   `json:"license,omitempty" yaml:"license,omitempty"`
	Keywords    []string `json:"keywords,omitempty" yaml:"keywords,omitempty"`

	Author *PluginAuthor `json:"author,omitempty" yaml:"author,omitempty"`

	// Settings is an opaque allowlisted settings-merge object (schema models it
	// as open additionalProperties), held raw to round-trip losslessly.
	Settings json.RawMessage `json:"settings,omitempty" yaml:"settings,omitempty"`

	Dependencies []PluginDependency `json:"dependencies,omitempty" yaml:"dependencies,omitempty"`

	// Component path overrides — string|array|object unions (see types below).
	Commands     *CommandsField   `json:"commands,omitempty" yaml:"commands,omitempty"`
	Agents       *PathOrPaths     `json:"agents,omitempty" yaml:"agents,omitempty"`
	Skills       *PathOrPaths     `json:"skills,omitempty" yaml:"skills,omitempty"`
	OutputStyles *PathOrPaths     `json:"outputStyles,omitempty" yaml:"outputStyles,omitempty"`
	Hooks        *HooksField      `json:"hooks,omitempty" yaml:"hooks,omitempty"`
	MCPServers   *MCPServersField `json:"mcpServers,omitempty" yaml:"mcpServers,omitempty"`
	LSPServers   *LSPServersField `json:"lspServers,omitempty" yaml:"lspServers,omitempty"`

	UserConfig map[string]PluginUserConfigField `json:"userConfig,omitempty" yaml:"userConfig,omitempty"`
	Channels   []PluginChannel                  `json:"channels,omitempty" yaml:"channels,omitempty"`

	// Themes/Monitors are the schemastore top-level placement; Experimental is
	// the docs-preferred nesting. Both are modeled so we re-emit whichever the
	// source used.
	Themes       *PathOrPaths        `json:"themes,omitempty" yaml:"themes,omitempty"`
	Monitors     *MonitorsField      `json:"monitors,omitempty" yaml:"monitors,omitempty"`
	Experimental *PluginExperimental `json:"experimental,omitempty" yaml:"experimental,omitempty"`

	// DisplayName / DefaultEnabled are docs-only (not in the schemastore schema)
	// but Claude loads them; modeled so real data isn't dropped.
	DisplayName    string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	DefaultEnabled *bool  `json:"defaultEnabled,omitempty" yaml:"defaultEnabled,omitempty"`

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
	Name  string `json:"name" yaml:"name"`
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
	URL   string `json:"url,omitempty" yaml:"url,omitempty"`
}

// PluginDependency is one `dependencies[]` entry: a string spec ("name",
// "name@marketplace", "name@^1.2.3") OR an object {name, marketplace, version}.
// Exactly one form is populated and preserved by (Un)MarshalJSON.
type PluginDependency struct {
	Ref         string `json:"-" yaml:"-"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	Marketplace string `json:"marketplace,omitempty" yaml:"marketplace,omitempty"`
	Version     string `json:"version,omitempty" yaml:"version,omitempty"`
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
	Values   []string
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
	Paths *PathOrPaths
	Map   map[string]CommandEntry
}

// CommandEntry is one named command in the object form.
type CommandEntry struct {
	Source       string   `json:"source,omitempty" yaml:"source,omitempty"`
	Content      string   `json:"content,omitempty" yaml:"content,omitempty"`
	Description  string   `json:"description,omitempty" yaml:"description,omitempty"`
	ArgumentHint string   `json:"argumentHint,omitempty" yaml:"argumentHint,omitempty"`
	Model        string   `json:"model,omitempty" yaml:"model,omitempty"`
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
	Path   string
	Events map[string][]HookMatcherGroup
	Raw    json.RawMessage
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
	Matcher string      `json:"matcher,omitempty" yaml:"matcher,omitempty"`
	Hooks   []HookEntry `json:"hooks" yaml:"hooks"`
}

// HookEntry is one hook action discriminated by Type (command|prompt|agent|
// http|mcp_tool). Variant fields are flattened with omitempty; per-type
// required/forbidden sets are enforced in plugin_validate.go.
type HookEntry struct {
	Type string `json:"type" yaml:"type"`

	Command string `json:"command,omitempty" yaml:"command,omitempty"`
	Shell   string `json:"shell,omitempty" yaml:"shell,omitempty"`

	Prompt string `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Model  string `json:"model,omitempty" yaml:"model,omitempty"`

	URL            string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers        map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	AllowedEnvVars []string          `json:"allowedEnvVars,omitempty" yaml:"allowedEnvVars,omitempty"`

	Server string          `json:"server,omitempty" yaml:"server,omitempty"`
	Tool   string          `json:"tool,omitempty" yaml:"tool,omitempty"`
	Input  json.RawMessage `json:"input,omitempty" yaml:"input,omitempty"`

	If string `json:"if,omitempty" yaml:"if,omitempty"`
	// Timeout is a pointer so an explicit "timeout": 0 (disable) round-trips
	// losslessly — a float64 with omitempty would silently drop the zero.
	Timeout       *float64 `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	StatusMessage string   `json:"statusMessage,omitempty" yaml:"statusMessage,omitempty"`
	Once          *bool    `json:"once,omitempty" yaml:"once,omitempty"`
	Async         *bool    `json:"async,omitempty" yaml:"async,omitempty"`
	AsyncRewake   *bool    `json:"asyncRewake,omitempty" yaml:"asyncRewake,omitempty"`
}

// MCPServersField models `mcpServers`: a path/MCPB string (Path), an inline
// name->config object (Servers), or an array form (Raw).
type MCPServersField struct {
	Path    string
	Servers map[string]MCPServerEntry
	Raw     json.RawMessage
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
	Type    string            `json:"type,omitempty" yaml:"type,omitempty"`
	Command string            `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []string          `json:"args,omitempty" yaml:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty" yaml:"env,omitempty"`

	URL           string            `json:"url,omitempty" yaml:"url,omitempty"`
	Headers       map[string]string `json:"headers,omitempty" yaml:"headers,omitempty"`
	HeadersHelper string            `json:"headersHelper,omitempty" yaml:"headersHelper,omitempty"`
	OAuth         *MCPServerOAuth   `json:"oauth,omitempty" yaml:"oauth,omitempty"`
}

// MCPServerOAuth is the sse/http oauth sub-block.
type MCPServerOAuth struct {
	ClientID string `json:"clientId,omitempty" yaml:"clientId,omitempty"`
	// Pointer so an explicit callbackPort:0 round-trips (omitempty would drop it).
	CallbackPort          *int     `json:"callbackPort,omitempty" yaml:"callbackPort,omitempty"`
	AuthServerMetadataURL string   `json:"authServerMetadataUrl,omitempty" yaml:"authServerMetadataUrl,omitempty"`
	Scopes                []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`
}

// LSPServersField models `lspServers`: a path string, an inline name->config
// object, or an array form (Raw).
type LSPServersField struct {
	Path    string
	Servers map[string]LSPServerEntry
	Raw     json.RawMessage
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
	Command               string            `json:"command" yaml:"command"`
	Args                  []string          `json:"args,omitempty" yaml:"args,omitempty"`
	ExtensionToLanguage   map[string]string `json:"extensionToLanguage" yaml:"extensionToLanguage"`
	Transport             string            `json:"transport,omitempty" yaml:"transport,omitempty"`
	Env                   map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
	InitializationOptions json.RawMessage   `json:"initializationOptions,omitempty" yaml:"initializationOptions,omitempty"`
	Settings              json.RawMessage   `json:"settings,omitempty" yaml:"settings,omitempty"`
	WorkspaceFolder       string            `json:"workspaceFolder,omitempty" yaml:"workspaceFolder,omitempty"`
	StartupTimeout        *int              `json:"startupTimeout,omitempty" yaml:"startupTimeout,omitempty"`
	MaxRestarts           *int              `json:"maxRestarts,omitempty" yaml:"maxRestarts,omitempty"`
}

// PluginUserConfigField is one typed enable-time prompt. Default is a
// string|number|boolean|string[] union held raw.
type PluginUserConfigField struct {
	Type        string          `json:"type" yaml:"type"`
	Title       string          `json:"title" yaml:"title"`
	Description string          `json:"description" yaml:"description"`
	Required    *bool           `json:"required,omitempty" yaml:"required,omitempty"`
	Default     json.RawMessage `json:"default,omitempty" yaml:"default,omitempty"`
	Multiple    *bool           `json:"multiple,omitempty" yaml:"multiple,omitempty"`
	Sensitive   *bool           `json:"sensitive,omitempty" yaml:"sensitive,omitempty"`
	Min         *float64        `json:"min,omitempty" yaml:"min,omitempty"`
	Max         *float64        `json:"max,omitempty" yaml:"max,omitempty"`
}

// PluginChannel declares an MCP-server-backed message channel.
type PluginChannel struct {
	Server      string                           `json:"server" yaml:"server"`
	DisplayName string                           `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	UserConfig  map[string]PluginUserConfigField `json:"userConfig,omitempty" yaml:"userConfig,omitempty"`
}

// MonitorsField models `monitors`: a `./*.json` path or an array of monitors.
type MonitorsField struct {
	Path    string
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
	Name        string `json:"name" yaml:"name"`
	Command     string `json:"command" yaml:"command"`
	Description string `json:"description" yaml:"description"`
	When        string `json:"when,omitempty" yaml:"when,omitempty"`
}

// PluginExperimental is the docs-preferred nesting for themes/monitors. Typed
// (not raw) so the derived inventory/governance can read it; unknown
// experimental keys are not separately preserved.
type PluginExperimental struct {
	Themes   *PathOrPaths   `json:"themes,omitempty" yaml:"themes,omitempty"`
	Monitors *MonitorsField `json:"monitors,omitempty" yaml:"monitors,omitempty"`
}
