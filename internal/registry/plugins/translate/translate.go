// Package translate converts plugin bundles between the canonical portable-core
// form (bundle.CanonicalBundle) and a concrete harness's on-disk file set.
//
// The canonical bundle is a SUPERSET that preserves every file — including the
// real .claude-plugin/plugin.json manifest — so canonical<->claude-code is
// lossless identity. Translating TO a harness that cannot represent a component
// drops it WITH A REPORT ENTRY — never silently, never as an error. Unrecognized
// paths default-pass (identity) so supporting files (skills/<n>/reference.md,
// AGENTS.md, the manifest, scripts) always survive. A non-claude-code adapter
// (e.g. a future codex adapter) expresses the manifest as a path+byte transform
// on .claude-plugin/plugin.json rather than regenerating it from metadata.
package translate

import (
	"errors"
	"sort"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
)

// Harness identifies a harness layout. Values match v1alpha1.PluginSpec.Harnesses.
type Harness string

const (
	HarnessClaudeCode Harness = "claude-code"
	HarnessCodex      Harness = "codex"
)

var (
	// ErrUnknownHarness is returned for a harness with no registered adapter and
	// no reserved name.
	ErrUnknownHarness = errors.New("translate: unknown harness")
	// ErrUnsupportedHarness is returned for a reserved-but-not-yet-implemented
	// harness (e.g. codex pending a verified plugin-layout spec).
	ErrUnsupportedHarness = errors.New("translate: harness translation not yet implemented")
)

// reservedHarnesses are known harness names; a miss here is ErrUnknownHarness,
// a miss in the adapter registry but presence here is ErrUnsupportedHarness.
var reservedHarnesses = map[Harness]struct{}{
	HarnessClaudeCode: {},
	HarnessCodex:      {},
}

// ToHarness materializes the canonical bundle into harness h's on-disk file set.
// It returns a new file map (b.Files is never mutated) plus an always-non-nil
// report of everything dropped/transformed. It errors only on an unknown/
// unsupported harness — never on a droppable component.
func ToHarness(h Harness, b *bundle.CanonicalBundle) (map[string][]byte, *TranslationReport, error) {
	a, err := lookup(h)
	if err != nil {
		return nil, nil, err
	}
	rep := &TranslationReport{Harness: h, Direction: DirToHarness}
	out := make(map[string][]byte, len(b.Files))
	for _, p := range sortedKeys(b.Files) {
		applyMapping(a.MapToHarness(p), p, b.Files[p], out, rep)
	}
	rep.sort()
	return out, rep, nil
}

// FromHarness ingests a harness h on-disk file set into a CanonicalBundle
// (canonical-by-construction: caller hashes via b.Bytes()). It reports any file
// dropped because it has no canonical home.
func FromHarness(h Harness, files map[string][]byte) (*bundle.CanonicalBundle, *TranslationReport, error) {
	a, err := lookup(h)
	if err != nil {
		return nil, nil, err
	}
	rep := &TranslationReport{Harness: h, Direction: DirFromHarness}
	out := make(map[string][]byte, len(files))
	for _, p := range sortedKeys(files) {
		applyMapping(a.MapFromHarness(p), p, files[p], out, rep)
	}
	rep.sort()
	return &bundle.CanonicalBundle{Files: out}, rep, nil
}

// applyMapping applies one path mapping to out + records report entries.
// Default-pass: a zero PathMapping (DestPath=="") means identity at the source
// path, so unrecognized files always survive.
func applyMapping(m PathMapping, src string, data []byte, out map[string][]byte, rep *TranslationReport) {
	if m.Drop {
		rep.addDrop(src, Classify(src), m.DropReason)
		return
	}
	dest := m.DestPath
	if dest == "" {
		dest = src
	}
	if m.Transform != nil {
		nd, notes, err := m.Transform(data)
		if err != nil {
			rep.addWarning(src, "transform failed, passed through: "+err.Error())
		} else {
			data = nd
			for _, n := range notes {
				rep.addTransform(src, dest, Classify(src), n)
			}
		}
	}
	if dest != src {
		rep.addTransform(src, dest, Classify(src), "moved")
	}
	out[dest] = data
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
