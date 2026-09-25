package bundle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ManifestPath is the canonical location of the plugin manifest within a bundle.
const ManifestPath = ".claude-plugin/plugin.json"

// agentPluginsMCPSchema is the $schema an Agent Plugins mcp.json must declare.
const agentPluginsMCPSchema = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"

// mcpConfigPaths maps each format to the MCP config file its harnesses read.
var mcpConfigPaths = map[v1alpha1.PluginFormat]string{
	v1alpha1.PluginFormatClaudePlugin: ".mcp.json",
	v1alpha1.PluginFormatAgentPlugins: "mcp.json",
}

// ParseManifest parses the bundle's manifest at path (the Claude manifest or
// the root Agent Plugins plugin.json) into the typed, faithful PluginManifest.
// Returns (nil, nil) when the bundle has no file at path, or (nil, err) when the
// manifest is present but malformed (the caller decides whether to fail).
func ParseManifest(b *CanonicalBundle, path string) (*v1alpha1.PluginManifest, error) {
	data, ok := b.Files[path]
	if !ok {
		return nil, nil
	}
	var m v1alpha1.PluginManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %v", ErrInvalidBundle, path, err)
	}
	return &m, nil
}

// BuildInventory indexes a canonical bundle into a PluginInventory: the skills,
// sub-agents, commands, MCP servers, hooks, and bin/ executables it actually
// ships — the legible governance risk surface, derived by scanning bundle files
// (not the author-supplied manifest). Best-effort: a malformed declarative file
// is skipped rather than failing the resolve. Output is deterministic (sorted).
// MCP servers come only from the MCP config file of format. Sub-agents,
// commands, and hooks are listed only for claude-plugin: Agent Plugins v1
// defines only skills and MCP servers, so kagent ignores the rest.
func BuildInventory(b *CanonicalBundle, format v1alpha1.PluginFormat) *v1alpha1.PluginInventory {
	m := &v1alpha1.PluginInventory{}
	claudeFormat := format == v1alpha1.PluginFormatClaudePlugin

	for _, p := range slices.Sorted(maps.Keys(b.Files)) {
		switch {
		case p == "SKILL.md" || (strings.HasPrefix(p, "skills/") && strings.HasSuffix(p, "/SKILL.md")):
			name, desc := parseSkillFrontmatter(b.Files[p])
			if name == "" {
				name = skillNameFromPath(p)
			}
			m.Skills = append(m.Skills, v1alpha1.PluginSkill{Name: name, Description: desc})
		case claudeFormat && strings.HasPrefix(p, "agents/") && strings.HasSuffix(p, ".md"):
			m.Agents = append(m.Agents, baseNameNoExt(p))
		case claudeFormat && strings.HasPrefix(p, "commands/") && strings.HasSuffix(p, ".md"):
			m.Commands = append(m.Commands, baseNameNoExt(p))
		case strings.HasPrefix(p, "bin/") && p != "bin/":
			m.Executables = append(m.Executables, strings.TrimPrefix(p, "bin/"))
		}
	}
	m.MCPServers = parseMCPServers(b, format)
	if claudeFormat {
		m.Hooks = parseHooks(b.Files["hooks/hooks.json"])
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

// parseMCPServers returns the sorted server names declared in the MCP config
// file of format. An absent or malformed file adds none.
func parseMCPServers(b *CanonicalBundle, format v1alpha1.PluginFormat) []string {
	config, ok := decodeMCPConfig(b.Files[mcpConfigPaths[format]], format)
	if !ok {
		return nil
	}
	return slices.Sorted(maps.Keys(config.MCPServers))
}

// mcpConfig is an MCP config file. Only an Agent Plugins mcp.json sets Schema.
type mcpConfig struct {
	Schema     string                     `json:"$schema"`
	MCPServers map[string]json.RawMessage `json:"mcpServers"`
}

// decodeMCPConfig decodes an MCP config file of format. An Agent Plugins
// mcp.json must also pass the spec's whole-file rules (§7.2.1), because kagent
// starts none of its servers when it fails them.
func decodeMCPConfig(data []byte, format v1alpha1.PluginFormat) (mcpConfig, bool) {
	var config mcpConfig
	if format != v1alpha1.PluginFormatAgentPlugins {
		return config, json.Unmarshal(data, &config) == nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&config)
	return config, err == nil && config.Schema == agentPluginsMCPSchema && config.MCPServers != nil
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
