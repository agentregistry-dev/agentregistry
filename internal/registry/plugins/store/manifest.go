package store

import (
	"encoding/json"
	"path"
	"sort"
	"strings"

	"sigs.k8s.io/yaml"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ParseManifest indexes a canonical bundle into a PluginManifest: the skills,
// sub-agents, commands, MCP servers, hooks, and bin/ executables it ships. It
// is best-effort — a malformed declarative file is skipped rather than failing
// the publish, since the manifest is a search/governance index, not the source
// of truth. Output is deterministic (sorted) for stable spec hashing.
func ParseManifest(b *CanonicalBundle) *v1alpha1.PluginManifest {
	m := &v1alpha1.PluginManifest{}

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
	sort.Strings(skillPaths)
	sort.Strings(agentPaths)
	sort.Strings(cmdPaths)
	sort.Strings(binPaths)

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
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", ""
	}
	var meta struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := yaml.Unmarshal([]byte(rest[:idx]), &meta); err != nil {
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
	sort.Strings(names)
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
	sort.Strings(events)

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
		if i := strings.Index(mid, "/"); i >= 0 {
			return mid[:i]
		}
		return mid
	}
	return ""
}
