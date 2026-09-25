package bundle

import (
	"encoding/json"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// claudeManifestComponents holds the Claude manifest keys that move or add
// components. Each key takes several JSON shapes, so each stays raw.
type claudeManifestComponents struct {
	Skills     json.RawMessage `json:"skills"`
	Commands   json.RawMessage `json:"commands"`
	Agents     json.RawMessage `json:"agents"`
	Hooks      json.RawMessage `json:"hooks"`
	MCPServers json.RawMessage `json:"mcpServers"`
}

// claudePlugin finds a Claude plugin's components where Claude Code loads them
// (code.claude.com/docs/en/plugins-reference).
type claudePlugin struct {
	bundle *CanonicalBundle
	claudeManifestComponents
}

func newClaudePlugin(b *CanonicalBundle) claudePlugin {
	p := claudePlugin{bundle: b}
	// format.Detect already rejected a manifest that is not a JSON object.
	_ = json.Unmarshal(b.Files[ManifestPath], &p.claudeManifestComponents)
	return p
}

func (p claudePlugin) inventory() *v1alpha1.PluginInventory {
	return &v1alpha1.PluginInventory{
		Skills:      skills(p.bundle, p.skillFileMatcher()),
		Agents:      markdownNames(p.bundle, p.agentFileMatcher()),
		Commands:    p.commands(),
		MCPServers:  p.mcpServers(),
		Hooks:       p.hooks(),
		Executables: executables(p.bundle),
	}
}

// skillFileMatcher matches skills/<name>/SKILL.md, the skills in each directory
// the skills key adds, and a root SKILL.md when nothing else declares skills.
func (p claudePlugin) skillFileMatcher() func(string) bool {
	dirs := manifestPaths(p.Skills)
	rootSkill := !isSet(p.Skills) && !p.bundle.HasDir("skills")
	return func(file string) bool {
		return isSkillsChild(file) || (rootSkill && file == "SKILL.md") ||
			slices.ContainsFunc(dirs, func(dir string) bool { return p.isSkillIn(file, dir) })
	}
}

// isSkillIn reports whether file is a skill of dir: dir/SKILL.md when dir
// holds one, else a dir/<name>/SKILL.md.
func (p claudePlugin) isSkillIn(file, dir string) bool {
	direct := path.Join(dir, "SKILL.md")
	if _, ok := p.bundle.Files[direct]; ok {
		return file == direct
	}
	return path.Base(file) == "SKILL.md" && path.Dir(path.Dir(file)) == dir
}

// agentFileMatcher matches agents/**.md, or only the files the agents key
// names, because that key replaces the default folder.
func (p claudePlugin) agentFileMatcher() func(string) bool {
	if !isSet(p.Agents) {
		return func(file string) bool { return strings.HasPrefix(file, "agents/") }
	}
	files := manifestPaths(p.Agents)
	return func(file string) bool { return slices.Contains(files, file) }
}

// commands lists commands/**.md, or what the commands key names instead: .md
// files, folders of them, or a map of command names.
func (p claudePlugin) commands() []string {
	if names, ok := objectKeys(p.Commands); ok {
		return names
	}
	roots := []string{"commands"}
	if isSet(p.Commands) {
		roots = manifestPaths(p.Commands)
	}
	return markdownNames(p.bundle, func(file string) bool { return underAny(file, roots) })
}

// mcpServers lists the servers in .mcp.json and in each mcpServers entry of
// the manifest. A later entry that reuses a name replaces the earlier server.
func (p claudePlugin) mcpServers() []string {
	names := mcpFileServerNames(p.bundle.Files[".mcp.json"])
	for _, entry := range manifestEntries(p.MCPServers) {
		names = append(names, p.mcpEntryServerNames(entry)...)
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// mcpEntryServerNames lists the servers of one mcpServers entry: an inline map,
// a .json file, or an MCP bundle. A bundle is listed by path, as its config is inside it.
func (p claudePlugin) mcpEntryServerNames(entry json.RawMessage) []string {
	if names, ok := objectKeys(entry); ok {
		return names
	}
	var ref string
	if json.Unmarshal(entry, &ref) != nil {
		return nil
	}
	if strings.HasSuffix(ref, ".mcpb") || strings.HasSuffix(ref, ".dxt") {
		return []string{ref}
	}
	return mcpFileServerNames(p.fileAt(ref))
}

// mcpFileServerNames lists the servers in an MCP config file, with or without
// the mcpServers wrapper. An absent or malformed file has none.
func mcpFileServerNames(data []byte) []string {
	var wrapped struct {
		MCPServers json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(data, &wrapped) == nil && wrapped.MCPServers != nil {
		names, _ := objectKeys(wrapped.MCPServers)
		return names
	}
	names, _ := objectKeys(data)
	return names
}

// hooks lists hooks/hooks.json merged with each hooks entry of the manifest:
// a hooks file, or an inline map of events.
func (p claudePlugin) hooks() []v1alpha1.PluginHook {
	configs := []hookEvents{hookFileEvents(p.bundle.Files["hooks/hooks.json"])}
	for _, entry := range manifestEntries(p.Hooks) {
		configs = append(configs, p.hookEntryEvents(entry))
	}
	return flattenHooks(configs)
}

func (p claudePlugin) hookEntryEvents(entry json.RawMessage) hookEvents {
	var ref string
	if json.Unmarshal(entry, &ref) == nil {
		return hookFileEvents(p.fileAt(ref))
	}
	var events hookEvents
	if json.Unmarshal(entry, &events) != nil {
		return nil
	}
	return events
}

// fileAt returns the file a manifest path names. It returns nil when the path
// leaves the plugin root or names no file.
func (p claudePlugin) fileAt(manifestPath string) []byte {
	file, ok := bundlePath(manifestPath)
	if !ok {
		return nil
	}
	return p.bundle.Files[file]
}

// isSet reports whether the manifest sets a key, even to an empty value.
func isSet(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

// manifestEntries splits a key's value into entries: the elements of an
// array, or the value itself.
func manifestEntries(raw json.RawMessage) []json.RawMessage {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) == nil {
		return entries
	}
	if !isSet(raw) {
		return nil
	}
	return []json.RawMessage{raw}
}

// manifestPaths lists the bundle paths that a path or an array of paths
// names. It skips a path that leaves the plugin root.
func manifestPaths(raw json.RawMessage) []string {
	var paths []string
	for _, entry := range manifestEntries(raw) {
		var ref string
		if json.Unmarshal(entry, &ref) != nil {
			continue
		}
		if file, ok := bundlePath(ref); ok {
			paths = append(paths, file)
		}
	}
	return paths
}

// bundlePath turns a manifest path such as "./extra/" into a bundle path, or
// "." for the plugin root. ok is false when the path leaves the plugin root.
func bundlePath(manifestPath string) (string, bool) {
	if path.IsAbs(manifestPath) || !staysInside(manifestPath) {
		return "", false
	}
	return path.Clean(manifestPath), true
}

// underAny reports whether file is one of roots or inside one of them.
func underAny(file string, roots []string) bool {
	return slices.ContainsFunc(roots, func(root string) bool {
		return root == "." || file == root || strings.HasPrefix(file, root+"/")
	})
}

// objectKeys returns the sorted keys of a JSON object. ok is false when raw is
// not an object.
func objectKeys(raw json.RawMessage) ([]string, bool) {
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, false
	}
	return slices.Sorted(maps.Keys(object)), true
}
