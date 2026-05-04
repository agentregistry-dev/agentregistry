package plugins

import (
	"fmt"
	"sort"
)

// Source identifies where a plugin was loaded from. Earlier sources win on conflict.
type Source int

const (
	SourceInTree  Source = iota // embedded in the arctl binary
	SourceProject               // ./.arctl/plugins/<name>/
	SourceUserHome              // $XDG_CONFIG_HOME/arctl/plugins/<name>/
)

func (s Source) String() string {
	switch s {
	case SourceInTree:
		return "in-tree"
	case SourceProject:
		return "project-local"
	case SourceUserHome:
		return "user"
	default:
		return "unknown"
	}
}

type registryEntry struct {
	plugin *Plugin
	source Source
}

type registryConflict struct {
	Key        string
	Winner     Source
	Loser      Source
	WinnerName string
	LoserName  string
}

// Registry indexes plugins by (type, framework, language). Earlier-source wins on conflict.
type Registry struct {
	entries   map[string]registryEntry
	conflicts []registryConflict
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{entries: make(map[string]registryEntry)}
}

func key(typ, framework, language string) string {
	return typ + "/" + framework + "/" + language
}

// Add inserts a plugin. If the key is already taken, the earlier-source plugin wins;
// the loser is recorded in Conflicts(). Returns nil for both wins and recorded losses.
func (r *Registry) Add(p *Plugin, src Source) error {
	if p == nil {
		return fmt.Errorf("plugin is nil")
	}
	k := key(p.Type, p.Framework, p.Language)
	if existing, ok := r.entries[k]; ok {
		// Earlier source wins. Source ordering: SourceInTree < SourceProject < SourceUserHome.
		if existing.source <= src {
			r.conflicts = append(r.conflicts, registryConflict{
				Key:        k,
				Winner:     existing.source,
				Loser:      src,
				WinnerName: existing.plugin.Name,
				LoserName:  p.Name,
			})
			return nil
		}
		// New source has higher priority — replace and record the displaced plugin.
		r.conflicts = append(r.conflicts, registryConflict{
			Key:        k,
			Winner:     src,
			Loser:      existing.source,
			WinnerName: p.Name,
			LoserName:  existing.plugin.Name,
		})
	}
	r.entries[k] = registryEntry{plugin: p, source: src}
	return nil
}

// Lookup finds a plugin by (type, framework, language).
func (r *Registry) Lookup(typ, framework, language string) (*Plugin, bool) {
	e, ok := r.entries[key(typ, framework, language)]
	if !ok {
		return nil, false
	}
	return e.plugin, true
}

// ListByType returns all plugins of the given type, sorted by Name for stable order.
func (r *Registry) ListByType(typ string) []*Plugin {
	var out []*Plugin
	for _, e := range r.entries {
		if e.plugin.Type == typ {
			out = append(out, e.plugin)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Conflicts returns all conflicts seen during Add. Useful for warning logs.
func (r *Registry) Conflicts() []registryConflict {
	return r.conflicts
}
