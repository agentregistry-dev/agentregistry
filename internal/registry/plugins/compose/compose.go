// Package compose flattens a composed plugin — an optional base bundle plus
// resolved registry components — into one canonical bundle. It is the compile
// step of plugin composability: a PURE function of its inputs (no I/O, no
// clock, no randomness), so any consumer holding the same pin set reproduces a
// byte-identical result.
//
// Placement rules (design: design-docs/PLUGIN_COMPOSABILITY_SPIKE.md §6):
//
//   - skill      → skills/<name>/**   whole-directory overlay-wins over base
//   - command    → commands/<name>.md whole-file overlay-wins
//   - mcp server → .mcp.json          keyed merge; overlay replaces same name
//   - instructions → AGENTS.md        append with a separator
//   - base .claude-plugin/plugin.json passes through untouched; a minimal
//     manifest is generated only when there is no base at all
//
// Every overlay-wins replacement is recorded in the Report so shadowing is
// visible, never silent.
package compose

import (
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const (
	mcpConfigPath    = ".mcp.json"
	agentsPath       = "AGENTS.md"
	manifestPath     = ".claude-plugin/plugin.json"
	instructionsSep  = "\n\n---\n\n"
	skillsPrefix     = "skills/"
	commandsPrefix   = "commands/"
	commandExtension = ".md"
)

// Inputs are the resolved pieces the controller hands to Compose. The typed
// fields mirror PluginSpec's composition block; each field's destination is
// disjoint from the others, so only intra-field order matters (and duplicate
// names within a field are rejected at admission).
type Inputs struct {
	// Base is the plugin's optional base bundle (nil for pure composition).
	Base *bundle.CanonicalBundle

	Skills       []Skill
	MCPServers   []MCPServer
	Commands     []Command
	Instructions *Instructions

	// Plugin identity, used only to generate a minimal manifest when Base is
	// nil.
	PluginName  string
	Title       string
	Description string
}

// Skill is one resolved Skill component: the file tree of its pinned repo.
type Skill struct {
	Name string
	// Files is the skill repo's tree (SKILL.md at the root), path-keyed the
	// same way as CanonicalBundle.Files.
	Files map[string][]byte
}

// MCPServer is one resolved MCPServer component, already mapped to its
// .mcp.json entry form.
type MCPServer struct {
	Name string
	// Entry is the JSON object placed at mcpServers[name].
	Entry json.RawMessage
}

// Command is one resolved Prompt-backed slash command.
type Command struct {
	Name string
	Body string
}

// Instructions is the resolved Prompt appended to AGENTS.md.
type Instructions struct {
	Name string
	Body string
}

// Report is the provenance record of one compose run. Empty slices mean a
// clean overlay with nothing shadowed.
type Report struct {
	// Placed lists every composed component and where it landed.
	Placed []Placement
	// Replaced lists base content an overlay replaced (overlay-wins events).
	Replaced []Replacement
}

// Placement records one component's destination.
type Placement struct {
	Kind string // v1alpha1 kind of the backing artifact
	Name string
	Dest string // bundle path (directory prefix for skills)
}

// Replacement records base content replaced by an overlay component.
type Replacement struct {
	Kind string
	Name string
	Dest string
	// Files is how many base files were replaced/removed at Dest.
	Files int
}

// Compose flattens inputs into a new canonical bundle. The base is never
// mutated. The result respects the bundle file-count/byte ceilings.
func Compose(in Inputs) (*bundle.CanonicalBundle, *Report, error) {
	files := map[string][]byte{}
	if in.Base != nil {
		maps.Copy(files, in.Base.Files)
	}
	report := &Report{}

	for _, s := range in.Skills {
		if err := overlaySkill(files, s, report); err != nil {
			return nil, nil, err
		}
	}
	for _, c := range in.Commands {
		overlayCommand(files, c, report)
	}
	if len(in.MCPServers) > 0 {
		if err := mergeMCPServers(files, in.MCPServers, report); err != nil {
			return nil, nil, err
		}
	}
	if in.Instructions != nil {
		appendInstructions(files, *in.Instructions, report)
	}
	if in.Base == nil {
		if err := generateManifest(files, in); err != nil {
			return nil, nil, err
		}
	}

	if err := checkCeilings(files); err != nil {
		return nil, nil, err
	}
	return &bundle.CanonicalBundle{Files: files}, report, nil
}

// overlaySkill places s's tree at skills/<name>/, atomically replacing any
// same-named base directory (whole-directory overlay-wins — base and overlay
// trees are never interleaved).
func overlaySkill(files map[string][]byte, s Skill, report *Report) error {
	dest := skillsPrefix + s.Name + "/"
	removed := 0
	for _, p := range sortedKeys(files) {
		if strings.HasPrefix(p, dest) {
			delete(files, p)
			removed++
		}
	}
	if removed > 0 {
		report.Replaced = append(report.Replaced, Replacement{Kind: v1alpha1.KindSkill, Name: s.Name, Dest: dest, Files: removed})
	}
	for _, rel := range sortedKeys(s.Files) {
		if err := validateComponentPath(rel); err != nil {
			return fmt.Errorf("skill %q: %w", s.Name, err)
		}
		files[dest+rel] = s.Files[rel]
	}
	report.Placed = append(report.Placed, Placement{Kind: v1alpha1.KindSkill, Name: s.Name, Dest: dest})
	return nil
}

// overlayCommand writes the command markdown at commands/<name>.md,
// replacing a same-named base file (whole-file overlay-wins).
func overlayCommand(files map[string][]byte, c Command, report *Report) {
	dest := commandsPrefix + c.Name + commandExtension
	if _, ok := files[dest]; ok {
		report.Replaced = append(report.Replaced, Replacement{Kind: v1alpha1.KindPrompt, Name: c.Name, Dest: dest, Files: 1})
	}
	files[dest] = []byte(c.Body)
	report.Placed = append(report.Placed, Placement{Kind: v1alpha1.KindPrompt, Name: c.Name, Dest: dest})
}

// mergeMCPServers performs the keyed structured merge into .mcp.json. The
// base document's unrelated top-level fields and unrelated server entries are
// preserved byte-for-byte; an overlay entry replaces a same-named base entry.
func mergeMCPServers(files map[string][]byte, servers []MCPServer, report *Report) error {
	doc := map[string]json.RawMessage{}
	entries := map[string]json.RawMessage{}
	if raw, ok := files[mcpConfigPath]; ok {
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("%w: base %s is not a JSON object: %v", bundle.ErrInvalidBundle, mcpConfigPath, err)
		}
		if rawServers, ok := doc["mcpServers"]; ok {
			if err := json.Unmarshal(rawServers, &entries); err != nil {
				return fmt.Errorf("%w: base %s mcpServers is not an object: %v", bundle.ErrInvalidBundle, mcpConfigPath, err)
			}
		}
	}
	for _, s := range servers {
		if _, ok := entries[s.Name]; ok {
			report.Replaced = append(report.Replaced, Replacement{Kind: v1alpha1.KindMCPServer, Name: s.Name, Dest: mcpConfigPath, Files: 1})
		}
		entries[s.Name] = s.Entry
		report.Placed = append(report.Placed, Placement{Kind: v1alpha1.KindMCPServer, Name: s.Name, Dest: mcpConfigPath})
	}
	rawServers, err := json.Marshal(entries) // map keys marshal sorted: deterministic
	if err != nil {
		return fmt.Errorf("encode %s mcpServers: %w", mcpConfigPath, err)
	}
	doc["mcpServers"] = rawServers
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", mcpConfigPath, err)
	}
	files[mcpConfigPath] = append(raw, '\n')
	return nil
}

// appendInstructions appends the prompt body to AGENTS.md (creating it when
// absent), separated from existing content.
func appendInstructions(files map[string][]byte, ins Instructions, report *Report) {
	body := strings.TrimRight(ins.Body, "\n") + "\n"
	if existing, ok := files[agentsPath]; ok && len(existing) > 0 {
		files[agentsPath] = append(append(
			[]byte(strings.TrimRight(string(existing), "\n")), []byte(instructionsSep)...), body...)
	} else {
		files[agentsPath] = []byte(body)
	}
	report.Placed = append(report.Placed, Placement{Kind: v1alpha1.KindPrompt, Name: ins.Name, Dest: agentsPath})
}

// generateManifest writes a minimal .claude-plugin/plugin.json for a pure
// composition (no base). A base bundle's manifest always passes through
// untouched instead.
func generateManifest(files map[string][]byte, in Inputs) error {
	m := struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
	}{Name: in.PluginName, Description: in.Description}
	if m.Description == "" {
		m.Description = in.Title
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode generated manifest: %w", err)
	}
	files[manifestPath] = append(raw, '\n')
	return nil
}

// checkCeilings enforces the bundle size bounds on the composed result — the
// per-source bounds in bundle.FromDir don't cover the sum of layers.
func checkCeilings(files map[string][]byte) error {
	if len(files) > bundle.MaxBundleFiles {
		return fmt.Errorf("%w: composed bundle has too many files (limit %d)", bundle.ErrInvalidBundle, bundle.MaxBundleFiles)
	}
	var total int64
	for _, b := range files {
		total += int64(len(b))
	}
	if total > bundle.MaxBundleBytes {
		return fmt.Errorf("%w: composed bundle exceeds %d bytes", bundle.ErrInvalidBundle, bundle.MaxBundleBytes)
	}
	return nil
}

// validateComponentPath mirrors the bundle path rules for component-supplied
// relative paths, so a hostile tree cannot escape its skills/<name>/ prefix.
func validateComponentPath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", bundle.ErrInvalidBundle)
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("%w: backslash in path %q", bundle.ErrInvalidBundle, p)
	}
	if path.IsAbs(p) {
		return fmt.Errorf("%w: absolute path %q", bundle.ErrInvalidBundle, p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("%w: non-clean path %q", bundle.ErrInvalidBundle, p)
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return fmt.Errorf("%w: parent traversal in path %q", bundle.ErrInvalidBundle, p)
	}
	return nil
}

// sortedKeys returns m's keys sorted, for deterministic iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
