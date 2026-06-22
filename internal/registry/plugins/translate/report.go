package translate

import "sort"

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
	sort.Slice(r.Dropped, func(i, j int) bool { return r.Dropped[i].SourcePath < r.Dropped[j].SourcePath })
	sort.Slice(r.Transformed, func(i, j int) bool {
		if r.Transformed[i].SourcePath != r.Transformed[j].SourcePath {
			return r.Transformed[i].SourcePath < r.Transformed[j].SourcePath
		}
		return r.Transformed[i].Note < r.Transformed[j].Note
	})
	sort.Slice(r.Warnings, func(i, j int) bool {
		if r.Warnings[i].Path != r.Warnings[j].Path {
			return r.Warnings[i].Path < r.Warnings[j].Path
		}
		return r.Warnings[i].Message < r.Warnings[j].Message
	})
}

// HasLoss reports whether anything was dropped (drops are loss; transforms aren't).
func (r *TranslationReport) HasLoss() bool { return len(r.Dropped) > 0 }

// IsClean reports a fully identity-shaped translation (no drops, no transforms).
func (r *TranslationReport) IsClean() bool {
	return len(r.Dropped) == 0 && len(r.Transformed) == 0
}
