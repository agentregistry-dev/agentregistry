package bundle

import (
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ManifestPath is the canonical location of the plugin manifest within a bundle.
const ManifestPath = ".claude-plugin/plugin.json"

// ParseManifest parses the bundle's real .claude-plugin/plugin.json into the
// typed, faithful PluginManifest (the canonical lingua-franca manifest).
// Returns (nil, nil) when the bundle ships no manifest, or (nil, err) when the
// manifest is present but malformed (the caller decides whether to fail).
func ParseManifest(b *CanonicalBundle) (*v1alpha1.PluginManifest, error) {
	data, ok := b.Files[ManifestPath]
	if !ok {
		return nil, nil
	}
	var m v1alpha1.PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrInvalidBundle, ManifestPath, err)
	}
	return &m, nil
}

// BuildInventory indexes a canonical bundle into a PluginInventory: the skills,
// sub-agents, commands, MCP servers, hooks, and bin/ executables it actually
// ships — the legible governance risk surface, derived by scanning bundle files
// (not the author-supplied manifest). Best-effort: a malformed declarative file
// is skipped rather than failing the resolve. Output is deterministic (sorted).
func BuildInventory(b *CanonicalBundle) *v1alpha1.PluginInventory {
	m := &v1alpha1.PluginInventory{}

	var skillPaths, agentPaths, cmdPaths, binPaths []string
	for p := range b.Files {
		switch {
		case p == "SKILL.md" || (strings.HasPrefix(p, "skills/") && strings.HasSuffix(p, "/SKILL.md")):
			skillPaths = append(skillPaths, p)
		case strings.HasPrefix(p, "agents/") && strings.HasSuffix(p, ".md"):
			agentPaths = append(agentPaths, p)
		case strings.HasPrefix(p, "commands/") && strings.HasSuffix(p, ".md"):
			cmdPaths = append(cmdPaths, p)
		case strings.HasPrefix(p, "bin/") && p != "bin/":
			binPaths = append(binPaths, p)
		}
	}
	slices.Sort(skillPaths)
	slices.Sort(agentPaths)
	slices.Sort(cmdPaths)
	slices.Sort(binPaths)

	for _, p := range skillPaths {
		name, desc := parseSkillFrontmatter(b.Files[p])
		if name == "" {
			name = skillNameFromPath(p)
		}
		m.Skills = append(m.Skills, v1alpha1.PluginSkill{Name: name, Description: desc})
	}
	for _, p := range agentPaths {
		m.Agents = append(m.Agents, baseNameNoExt(p))
	}
	for _, p := range cmdPaths {
		m.Commands = append(m.Commands, baseNameNoExt(p))
	}
	for _, p := range binPaths {
		m.Executables = append(m.Executables, strings.TrimPrefix(p, "bin/"))
	}
	if data, ok := b.Files[".mcp.json"]; ok {
		m.MCPServers = parseMCPServers(data)
	}
	if data, ok := b.Files["hooks/hooks.json"]; ok {
		m.Hooks = parseHooks(data)
	}
	return m
}

// parseSkillFrontmatter extracts name/description from a SKILL.md YAML
// frontmatter block (--- ... ---). Returns empties on any parse failure.
func parseSkillFrontmatter(content []byte) (name, desc string) {
	s := string(content)
	if !strings.HasPrefix(s, "---") {
		return "", ""
	}
	rest := s[3:]
	frontmatter, _, ok := strings.Cut(rest, "\n---")
	if !ok {
		return "", ""
	}
	var meta struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return "", ""
	}
	return meta.Name, meta.Description
}

// parseMCPServers returns the sorted server names declared in a .mcp.json file.
func parseMCPServers(data []byte) []string {
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	names := make([]string, 0, len(doc.MCPServers))
	for k := range doc.MCPServers {
		names = append(names, k)
	}
	slices.Sort(names)
	return names
}

// parseHooks flattens a hooks.json ({hooks:{<Event>:[{hooks:[{type}]}]}}) into
// a deduplicated, sorted list of (event, handler-type) pairs.
func parseHooks(data []byte) []v1alpha1.PluginHook {
	var doc struct {
		Hooks map[string][]struct {
			Hooks []struct {
				Type string `json:"type"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil
	}
	events := make([]string, 0, len(doc.Hooks))
	for ev := range doc.Hooks {
		events = append(events, ev)
	}
	slices.Sort(events)

	seen := map[string]bool{}
	var out []v1alpha1.PluginHook
	for _, ev := range events {
		for _, matcher := range doc.Hooks[ev] {
			if len(matcher.Hooks) == 0 {
				if key := ev + "|"; !seen[key] {
					seen[key] = true
					out = append(out, v1alpha1.PluginHook{Event: ev})
				}
				continue
			}
			for _, h := range matcher.Hooks {
				if key := ev + "|" + h.Type; !seen[key] {
					seen[key] = true
					out = append(out, v1alpha1.PluginHook{Event: ev, Type: h.Type})
				}
			}
		}
	}
	return out
}

func baseNameNoExt(p string) string {
	b := path.Base(p)
	return strings.TrimSuffix(b, path.Ext(b))
}

func skillNameFromPath(p string) string {
	if strings.HasPrefix(p, "skills/") && strings.HasSuffix(p, "/SKILL.md") {
		mid := strings.TrimSuffix(strings.TrimPrefix(p, "skills/"), "/SKILL.md")
		if name, _, ok := strings.Cut(mid, "/"); ok {
			return name
		}
		return mid
	}
	return ""
}
