package translate

import (
	"fmt"
	"sort"
)

// ByteTransform rewrites a file's bytes during translation. It returns the new
// bytes and zero or more human-readable notes for the report. A non-nil error
// is handled leniently by the orchestration (original bytes pass through, a
// warning is recorded). Any transform can surface report entries, so no
// per-kind special-casing leaks into the orchestration.
type ByteTransform func(in []byte) (out []byte, notes []string, err error)

// PathMapping is an adapter's decision for one source path. The zero value
// (DestPath=="" , Transform==nil, Drop==false) means default-pass: identity at
// the source path. Adapters express only the exceptions.
type PathMapping struct {
	DestPath   string        // destination path; "" => same as source (identity)
	Transform  ByteTransform // nil => bytes unchanged
	Drop       bool          // true => omit from output and record a DroppedComponent
	DropReason string        // why, for the report (set when Drop)
}

// Adapter encapsulates one harness's on-disk conventions. Implementations
// register via Register in an init(). They must be stateless and concurrency-safe.
type Adapter interface {
	Harness() Harness
	// ManifestPath is the harness manifest's relative path, e.g.
	// ".claude-plugin/plugin.json".
	ManifestPath() string
	// MapToHarness maps a canonical path to its harness on-disk path. Return the
	// zero PathMapping for default-pass (identity).
	MapToHarness(canonicalPath string) PathMapping
	// MapFromHarness is the inverse. Return the zero PathMapping for default-pass.
	MapFromHarness(harnessPath string) PathMapping
	// GenerateManifest produces the harness manifest bytes from canonical
	// metadata. Output must be deterministic (stable key order).
	GenerateManifest(meta PluginMeta) ([]byte, error)
	// ParseManifest recovers PluginMeta from harness manifest bytes; unknown
	// fields are stashed into meta.Extras[Harness].
	ParseManifest(manifestBytes []byte) (PluginMeta, error)
}

var registry = map[Harness]Adapter{}

// Register adds an adapter. Panics on duplicate (init-time programming error).
func Register(a Adapter) {
	h := a.Harness()
	if _, dup := registry[h]; dup {
		panic(fmt.Sprintf("translate: adapter for harness %q already registered", h))
	}
	registry[h] = a
}

// Harnesses returns the sorted list of harnesses with a registered adapter.
func Harnesses() []Harness {
	out := make([]Harness, 0, len(registry))
	for h := range registry {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// lookup resolves an adapter, distinguishing unknown from reserved-unimplemented.
func lookup(h Harness) (Adapter, error) {
	if a, ok := registry[h]; ok {
		return a, nil
	}
	if _, reserved := reservedHarnesses[h]; reserved {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedHarness, h)
	}
	return nil, fmt.Errorf("%w: %q", ErrUnknownHarness, h)
}
