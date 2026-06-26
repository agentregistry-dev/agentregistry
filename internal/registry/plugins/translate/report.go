package translate

import "slices"

// Direction is the translation direction.
type Direction string

const (
	DirToHarness   Direction = "to_harness"   // canonical -> harness
	DirFromHarness Direction = "from_harness" // harness -> canonical
)

// TranslationReport records every non-identity outcome: components dropped (no
// destination), files transformed (moved or rewritten), and non-fatal warnings.
// It is always non-nil; an empty report means a clean, identity-shaped, lossless
// translation. Slices are sorted before return for stable golden comparison.
type TranslationReport struct {
	Harness     Harness            `json:"harness"`
	Direction   Direction          `json:"direction"`
	Dropped     []DroppedComponent `json:"dropped,omitempty"`
	Transformed []TransformedFile  `json:"transformed,omitempty"`
	Warnings    []Warning          `json:"warnings,omitempty"`
}

// DroppedComponent is one source path that had no destination in the target.
type DroppedComponent struct {
	SourcePath string        `json:"sourcePath"`
	Kind       ComponentKind `json:"kind"`
	Reason     string        `json:"reason"`
}

// TransformedFile is one path moved and/or byte-rewritten (DestPath may equal
// SourcePath when only bytes changed; "" when consumed, e.g. a manifest).
type TransformedFile struct {
	SourcePath string        `json:"sourcePath"`
	DestPath   string        `json:"destPath"`
	Kind       ComponentKind `json:"kind"`
	Note       string        `json:"note,omitempty"`
}

// Warning is a non-fatal issue (lenient transform failure, manifest fallback).
type Warning struct {
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func (r *TranslationReport) addDrop(src string, kind ComponentKind, reason string) {
	r.Dropped = append(r.Dropped, DroppedComponent{SourcePath: src, Kind: kind, Reason: reason})
}

func (r *TranslationReport) addTransform(src, dest string, kind ComponentKind, note string) {
	r.Transformed = append(r.Transformed, TransformedFile{SourcePath: src, DestPath: dest, Kind: kind, Note: note})
}

func (r *TranslationReport) addWarning(path, msg string) {
	r.Warnings = append(r.Warnings, Warning{Path: path, Message: msg})
}

// sort orders all slices deterministically (source iteration is unordered).
func (r *TranslationReport) sort() {
	slices.SortFunc(r.Dropped, func(a, b DroppedComponent) int {
		return cmpString(a.SourcePath, b.SourcePath)
	})
	slices.SortFunc(r.Transformed, func(a, b TransformedFile) int {
		if a.SourcePath != b.SourcePath {
			return cmpString(a.SourcePath, b.SourcePath)
		}
		return cmpString(a.Note, b.Note)
	})
	slices.SortFunc(r.Warnings, func(a, b Warning) int {
		if a.Path != b.Path {
			return cmpString(a.Path, b.Path)
		}
		return cmpString(a.Message, b.Message)
	})
}

func cmpString(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

// HasLoss reports whether anything was dropped (drops are loss; transforms aren't).
func (r *TranslationReport) HasLoss() bool { return len(r.Dropped) > 0 }

// IsClean reports a fully identity-shaped translation (no drops, no transforms).
func (r *TranslationReport) IsClean() bool {
	return len(r.Dropped) == 0 && len(r.Transformed) == 0
}
