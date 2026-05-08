package frameworks

import (
	"fmt"
	"sort"
)

// Source identifies where a framework was loaded from. Earlier sources win on conflict.
type Source int

const (
	SourceInTree   Source = iota // embedded in the arctl binary
	SourceProject                // ./.arctl/frameworks/<name>/
	SourceUserHome               // $XDG_CONFIG_HOME/arctl/frameworks/<name>/
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
	framework *Framework
	source Source
}

type registryConflict struct {
	Key        string
	Winner     Source
	Loser      Source
	WinnerName string
	LoserName  string
}

// Registry indexes frameworks by (type, framework, language). Earlier-source wins on conflict.
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

// Add inserts a framework. If the key is already taken, the earlier-source framework wins;
// the loser is recorded in Conflicts(). Returns nil for both wins and recorded losses.
func (r *Registry) Add(p *Framework, src Source) error {
	if p == nil {
		return fmt.Errorf("framework is nil")
	}
	k := key(p.Type, p.Framework, p.Language)
	if existing, ok := r.entries[k]; ok {
		// Earlier source wins. Source ordering: SourceInTree < SourceProject < SourceUserHome.
		if existing.source <= src {
			r.conflicts = append(r.conflicts, registryConflict{
				Key:        k,
				Winner:     existing.source,
				Loser:      src,
				WinnerName: existing.framework.Name,
				LoserName:  p.Name,
			})
			return nil
		}
		// New source has higher priority — replace and record the displaced framework.
		r.conflicts = append(r.conflicts, registryConflict{
			Key:        k,
			Winner:     src,
			Loser:      existing.source,
			WinnerName: p.Name,
			LoserName:  existing.framework.Name,
		})
	}
	r.entries[k] = registryEntry{framework: p, source: src}
	return nil
}

// Lookup finds a framework by (type, framework, language).
func (r *Registry) Lookup(typ, framework, language string) (*Framework, bool) {
	e, ok := r.entries[key(typ, framework, language)]
	if !ok {
		return nil, false
	}
	return e.framework, true
}

// ListByType returns all frameworks of the given type, sorted by Name for stable order.
func (r *Registry) ListByType(typ string) []*Framework {
	var out []*Framework
	for _, e := range r.entries {
		if e.framework.Type == typ {
			out = append(out, e.framework)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Conflicts returns all conflicts seen during Add. Useful for warning logs.
func (r *Registry) Conflicts() []registryConflict {
	return r.conflicts
}
