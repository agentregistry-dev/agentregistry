package bundle

import (
	"cmp"
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

// ParseManifest parses the manifest at path into the typed PluginManifest. It
// drops each key the type cannot parse, because the format rules skip it. It
// returns (nil, nil) when path has no file.
func ParseManifest(b *CanonicalBundle, path string) (*v1alpha1.PluginManifest, error) {
	data, ok := b.Files[path]
	if !ok {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %w", ErrInvalidBundle, path, err)
	}
	dropUnparsableKeys(fields)
	parsable, err := json.Marshal(fields)
	if err != nil {
		return nil, fmt.Errorf("%w: parse %s: %w", ErrInvalidBundle, path, err)
	}
	var m v1alpha1.PluginManifest
	if err := json.Unmarshal(parsable, &m); err != nil {
		return nil, fmt.Errorf("%w: parse %s: %w", ErrInvalidBundle, path, err)
	}
	return &m, nil
}

// dropUnparsableKeys deletes each key whose value the typed manifest rejects.
func dropUnparsableKeys(fields map[string]json.RawMessage) {
	for key, value := range fields {
		single, err := json.Marshal(map[string]json.RawMessage{key: value})
		if err != nil || json.Unmarshal(single, &v1alpha1.PluginManifest{}) != nil {
			delete(fields, key)
		}
	}
}

// BuildInventory lists the skills, agents, commands, MCP servers, hooks, and
// bin/ executables that format loads. A malformed file is skipped. Output is sorted.
func BuildInventory(b *CanonicalBundle, format v1alpha1.PluginFormat) *v1alpha1.PluginInventory {
	if format == v1alpha1.PluginFormatClaudePlugin {
		return newClaudePlugin(b).inventory()
	}
	return agentPluginsInventory(b)
}

// skills lists each SKILL.md that isSkill matches, in path order.
func skills(b *CanonicalBundle, isSkill func(string) bool) []v1alpha1.PluginSkill {
	var out []v1alpha1.PluginSkill
	for _, p := range slices.Sorted(maps.Keys(b.Files)) {
		if isSkill(p) {
			out = append(out, skillAt(b, p))
		}
	}
	return out
}

// skillAt names the skill at p by its frontmatter, else by its directory.
func skillAt(b *CanonicalBundle, p string) v1alpha1.PluginSkill {
	name, desc := parseSkillFrontmatter(b.Files[p])
	if name == "" {
		name = skillNameFromPath(p)
	}
	return v1alpha1.PluginSkill{Name: name, Description: desc}
}

// markdownNames lists the base name of each .md file that matches, in path
// order.
func markdownNames(b *CanonicalBundle, matches func(string) bool) []string {
	var out []string
	for _, p := range slices.Sorted(maps.Keys(b.Files)) {
		if strings.HasSuffix(p, ".md") && matches(p) {
			out = append(out, baseNameNoExt(p))
		}
	}
	return out
}

func executables(b *CanonicalBundle) []string {
	var out []string
	for _, p := range slices.Sorted(maps.Keys(b.Files)) {
		if name, ok := strings.CutPrefix(p, "bin/"); ok {
			out = append(out, name)
		}
	}
	return out
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

// hookEvents is a hooks config: each event's matcher groups.
type hookEvents map[string][]hookGroup

// hookGroup is one matcher group and its handlers.
type hookGroup struct {
	Hooks []struct {
		Type string `json:"type"`
	} `json:"hooks"`
}

// hookFileEvents reads a hooks file ({hooks:{<Event>:[{hooks:[{type}]}]}}).
// An absent or malformed file has none.
func hookFileEvents(data []byte) hookEvents {
	var doc struct {
		Hooks hookEvents `json:"hooks"`
	}
	if json.Unmarshal(data, &doc) != nil {
		return nil
	}
	return doc.Hooks
}

// flattenHooks lists each (event, handler type) pair once, sorted.
func flattenHooks(configs []hookEvents) []v1alpha1.PluginHook {
	var out []v1alpha1.PluginHook
	for _, config := range configs {
		for event, groups := range config {
			out = append(out, eventHooks(event, groups)...)
		}
	}
	slices.SortFunc(out, func(a, b v1alpha1.PluginHook) int {
		return cmp.Or(cmp.Compare(a.Event, b.Event), cmp.Compare(a.Type, b.Type))
	})
	return slices.Compact(out)
}

// eventHooks lists the handler types of one event. A matcher group with no
// handlers lists the event alone.
func eventHooks(event string, groups []hookGroup) []v1alpha1.PluginHook {
	var out []v1alpha1.PluginHook
	for _, group := range groups {
		if len(group.Hooks) == 0 {
			out = append(out, v1alpha1.PluginHook{Event: event})
		}
		for _, h := range group.Hooks {
			out = append(out, v1alpha1.PluginHook{Event: event, Type: h.Type})
		}
	}
	return out
}

func baseNameNoExt(p string) string {
	b := path.Base(p)
	return strings.TrimSuffix(b, path.Ext(b))
}

// isSkillsChild reports whether p is skills/<name>/SKILL.md. Agent Plugins
// §7.1 forbids searching deeper, and Claude Code does not either.
func isSkillsChild(p string) bool {
	return path.Base(p) == "SKILL.md" && path.Dir(path.Dir(p)) == "skills"
}

// skillNameFromPath names a skill by the directory that holds its SKILL.md.
// A root SKILL.md has no such name.
func skillNameFromPath(p string) string {
	if dir := path.Dir(p); dir != "." {
		return path.Base(dir)
	}
	return ""
}
