// Package translate converts plugin bundles between the canonical portable-core
// form (store.CanonicalBundle) and a concrete harness's on-disk file set.
//
// The canonical bundle is a SUPERSET that preserves every file, so
// canonical<->claude-code is lossless identity (only the harness manifest is
// added on the way out and consumed on the way in). Translating TO a harness
// that cannot represent a component drops it WITH A REPORT ENTRY — never
// silently, never as an error. Unrecognized paths default-pass (identity) so
// supporting files (skills/<n>/reference.md, AGENTS.md, scripts) always survive.
package translate

import (
	"errors"
	"sort"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/store"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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

// PluginMeta is the harness-neutral metadata used to generate (or recovered when
// parsing) a harness manifest. It is not the full v1alpha1.Plugin; callers
// project the relevant fields. Spec.Content/Manifest are registry-owned and
// never round-tripped through here.
type PluginMeta struct {
	Name        string
	Version     string
	Title       string
	Description string
	// Extras carries harness-manifest fields with no canonical home, namespaced
	// by harness so they don't collide. Preserved on same-harness round-trips;
	// lost when targeting a different harness.
	Extras map[Harness]map[string]any
}

// MetaFromPlugin projects a v1alpha1.Plugin into PluginMeta for translation.
func MetaFromPlugin(p *v1alpha1.Plugin) PluginMeta {
	return PluginMeta{
		Name:        p.Metadata.Name,
		Version:     p.Metadata.Tag,
		Title:       p.Spec.Title,
		Description: p.Spec.Description,
	}
}

// ToHarness materializes the canonical bundle into harness h's on-disk file set.
// It returns a new file map (b.Files is never mutated) plus an always-non-nil
// report of everything dropped/transformed. It errors only on an unknown/
// unsupported harness or a manifest-generation failure — never on a droppable
// component.
func ToHarness(h Harness, b *store.CanonicalBundle, meta PluginMeta) (map[string][]byte, *TranslationReport, error) {
	a, err := lookup(h)
	if err != nil {
		return nil, nil, err
	}
	rep := &TranslationReport{Harness: h, Direction: DirToHarness}
	out := make(map[string][]byte, len(b.Files)+1)

	for _, p := range sortedKeys(b.Files) {
		applyMapping(a.MapToHarness(p), p, b.Files[p], out, rep)
	}

	manifest, err := a.GenerateManifest(meta)
	if err != nil {
		return nil, nil, err
	}
	out[a.ManifestPath()] = manifest

	rep.sort()
	return out, rep, nil
}

// FromHarness ingests a harness h on-disk file set into a CanonicalBundle
// (canonical-by-construction: caller hashes via b.Bytes()). It recovers
// PluginMeta from the harness manifest and reports harness-only files dropped
// because they have no canonical home.
func FromHarness(h Harness, files map[string][]byte) (*store.CanonicalBundle, PluginMeta, *TranslationReport, error) {
	a, err := lookup(h)
	if err != nil {
		return nil, PluginMeta{}, nil, err
	}
	rep := &TranslationReport{Harness: h, Direction: DirFromHarness}
	out := make(map[string][]byte, len(files))
	var meta PluginMeta

	manifestPath := a.ManifestPath()
	for _, p := range sortedKeys(files) {
		if p == manifestPath {
			if m, perr := a.ParseManifest(files[p]); perr != nil {
				rep.addWarning(p, "manifest parse failed: "+perr.Error())
			} else {
				meta = m
			}
			rep.addTransform(p, "", Classify(p), "manifest consumed into metadata")
			continue
		}
		applyMapping(a.MapFromHarness(p), p, files[p], out, rep)
	}

	rep.sort()
	return &store.CanonicalBundle{Files: out}, meta, rep, nil
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
			// Lenient (mirrors store.ParseManifest): pass original bytes through.
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
