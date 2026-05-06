// internal/cli/plugins/picker.go
package plugins

import (
	"fmt"
	"sort"
	"strings"
)

// PickOpts drives plugin selection: explicit flags, interactive picker, or both.
type PickOpts struct {
	Registry       *Registry
	Type           string // "agent" or "mcp"
	Framework      string // optional, from --framework flag
	Language       string // optional, from --language flag
	NonInteractive bool   // when true, never prompt; error if ambiguous
}

// Pick resolves a plugin given user-supplied flags and/or the registry's options.
// If both flags are set, lookup is direct. Otherwise:
//   - interactive: present a picker (TODO: hook bubbletea in a follow-up; for v1 use simple prompts)
//   - non-interactive: error with the available options listed.
func Pick(opts PickOpts) (*Plugin, error) {
	if opts.Framework != "" && opts.Language != "" {
		p, ok := opts.Registry.Lookup(opts.Type, opts.Framework, opts.Language)
		if !ok {
			return nil, fmt.Errorf("no %s plugin for framework=%q language=%q", opts.Type, opts.Framework, opts.Language)
		}
		return p, nil
	}

	candidates := opts.Registry.ListByType(opts.Type)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no plugins available for type=%q", opts.Type)
	}

	if opts.NonInteractive {
		return nil, fmt.Errorf("ambiguous plugin selection (need --framework/--language). Available: %s", listPlugins(candidates))
	}

	return interactivePick(candidates, opts.Framework, opts.Language)
}

func listPlugins(plugins []*Plugin) string {
	parts := make([]string, 0, len(plugins))
	for _, p := range plugins {
		parts = append(parts, fmt.Sprintf("%s/%s", p.Framework, p.Language))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// interactivePick is a placeholder for the bubbletea TUI. For now it just
// returns the first candidate when there is only one, and errors when ambiguous.
// Replaced by a real TUI in a follow-up commit (Task 4.2).
func interactivePick(candidates []*Plugin, framework, language string) (*Plugin, error) {
	// Filter by partial flags.
	filtered := candidates[:0:0]
	for _, p := range candidates {
		if framework != "" && p.Framework != framework {
			continue
		}
		if language != "" && p.Language != language {
			continue
		}
		filtered = append(filtered, p)
	}
	if len(filtered) == 1 {
		return filtered[0], nil
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no plugin matches the supplied flags")
	}
	return nil, fmt.Errorf("multiple plugins match (need --framework/--language to disambiguate). Available: %s", listPlugins(filtered))
}
