// internal/cli/frameworks/picker.go
package frameworks

import (
	"fmt"
	"slices"
	"strings"
)

// PickOpts drives framework selection: explicit flags, interactive picker, or both.
type PickOpts struct {
	Registry       *Registry
	Type           string // "agent" or "mcp"
	Framework      string // optional, from --framework flag
	Language       string // optional, from --language flag
	NonInteractive bool   // when true, never prompt; error if ambiguous
}

// Pick resolves a framework given user-supplied flags and/or the registry's options.
// If both flags are set, lookup is direct. Otherwise:
//   - interactive: present a picker (TODO: hook bubbletea in a follow-up; for v1 use simple prompts)
//   - non-interactive: error with the available options listed.
func Pick(opts PickOpts) (*Framework, error) {
	if opts.Framework != "" && opts.Language != "" {
		p, ok := opts.Registry.Lookup(opts.Type, opts.Framework, opts.Language)
		if !ok {
			return nil, fmt.Errorf("no %s framework for framework=%q language=%q", opts.Type, opts.Framework, opts.Language)
		}
		return p, nil
	}

	candidates := opts.Registry.ListByType(opts.Type)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no frameworks available for type=%q", opts.Type)
	}

	if opts.NonInteractive {
		return nil, fmt.Errorf("ambiguous framework selection (need --framework/--language). Available: %s", listFrameworks(candidates))
	}

	return interactivePick(candidates, opts.Framework, opts.Language)
}

func listFrameworks(frameworks []*Framework) string {
	parts := make([]string, 0, len(frameworks))
	for _, p := range frameworks {
		parts = append(parts, fmt.Sprintf("%s/%s", p.Framework, p.Language))
	}
	slices.Sort(parts)
	return strings.Join(parts, ", ")
}

// interactivePick filters candidates by any partial flags and presents a
// bubbletea picker. The picker is shown even when only one candidate
// remains, for consistency and discoverability (per design).
func interactivePick(candidates []*Framework, framework, language string) (*Framework, error) {
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
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no framework matches the supplied flags")
	}
	return runPickerTUI(filtered)
}
