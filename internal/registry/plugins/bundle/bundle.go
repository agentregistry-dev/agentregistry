// Package bundle is the in-memory representation of a plugin's portable core:
// a flat, path-keyed set of files (SKILL.md, AGENTS.md, .mcp.json, mcp.json,
// hooks/*, commands/*, agents/*, bin/*, and the real plugin.json or
// .claude-plugin/plugin.json manifest) plus every directory path. It is
// loaded from a checked-out source tree (FromDir), scanned to derive the typed
// manifest (ParseManifest) and the governance inventory (BuildInventory), and
// translated into a harness's on-disk layout at deploy time.
//
// The registry does NOT host bundles. A Plugin's spec points at an external
// source (a pinned git commit or OCI digest); the controller resolves that
// pointer and records the derived manifest/inventory in status, and deploys
// materialize the harness filesystem from the source. This package owns the
// in-memory bundle shape only — it performs no network or registry I/O.
package bundle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// ErrInvalidBundle is returned when bundle content cannot be represented
// canonically (path traversal, backslash, absolute or non-clean path), when a
// declared file (e.g. the manifest) is present but malformed, or when the
// source tree exceeds the bundle size/file-count ceilings.
var ErrInvalidBundle = errors.New("invalid plugin bundle")

const (
	// MaxBundleFiles caps the number of regular files FromDir will load from a
	// source tree — a guard against a hostile/huge repo exhausting memory.
	MaxBundleFiles = 10_000
	// MaxBundleBytes caps the total bytes FromDir will load into memory.
	MaxBundleBytes int64 = 128 << 20 // 128 MiB
)

// CanonicalBundle is the portable core of a plugin: a flat, path-keyed set of
// files. It is NOT harness-specific; translation to a harness's on-disk layout
// happens at deploy time.
//
// Paths are clean, relative, forward-slash separated (no leading "/" or "..").
type CanonicalBundle struct {
	Files map[string][]byte
	// Dirs holds every directory path, even one that holds no kept file.
	Dirs map[string]bool
}

// HasDir reports whether dir is a directory in b.
func (b *CanonicalBundle) HasDir(dir string) bool {
	if b.Dirs[dir] {
		return true
	}
	for p := range b.Files {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// FromDir reads a checked-out plugin source tree rooted at dir into a
// CanonicalBundle. It skips symlinks and the .git directory, and records every
// directory in Dirs. Every file path is normalized to forward slashes and
// traversal-checked.
func FromDir(dir string) (*CanonicalBundle, error) {
	return fromDir(dir, MaxBundleFiles, MaxBundleBytes)
}

// fromDir is FromDir with explicit limits, so tests can exercise the ceilings
// without materializing huge trees.
func fromDir(dir string, maxFiles int, maxBytes int64) (*CanonicalBundle, error) {
	w := &treeWalker{root: dir, maxFiles: maxFiles, maxBytes: maxBytes,
		bundle: &CanonicalBundle{Files: map[string][]byte{}, Dirs: map[string]bool{}}}
	if err := filepath.WalkDir(dir, w.visit); err != nil {
		// Preserve a wrapped ErrInvalidBundle (size/traversal) as terminal;
		// wrap any other walk/IO error so the caller sees a bundle error.
		if errors.Is(err, ErrInvalidBundle) {
			return nil, err
		}
		return nil, fmt.Errorf("%w: read source tree: %w", ErrInvalidBundle, err)
	}
	return w.bundle, nil
}

// treeWalker loads one source tree into a bundle within the size limits.
type treeWalker struct {
	root       string
	maxFiles   int
	maxBytes   int64
	totalBytes int64
	bundle     *CanonicalBundle
}

func (w *treeWalker) visit(p string, d fs.DirEntry, err error) error {
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(w.root, p)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if d.IsDir() {
		return w.visitDir(rel, d)
	}
	// Skip symlinks (and any other irregular files) to avoid traversal out
	// of the source tree via a malicious link.
	if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
		return nil
	}
	return w.visitFile(rel, d)
}

// visitDir records a directory. It skips .git and the root.
func (w *treeWalker) visitDir(rel string, d fs.DirEntry) error {
	if d.Name() == ".git" {
		return filepath.SkipDir
	}
	if rel == "." {
		return nil
	}
	if err := validateBundlePath(rel); err != nil {
		return err
	}
	w.bundle.Dirs[rel] = true
	return nil
}

func (w *treeWalker) visitFile(rel string, d fs.DirEntry) error {
	if err := validateBundlePath(rel); err != nil {
		return err
	}
	if err := w.checkLimits(d); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(w.root, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	w.totalBytes += int64(len(data))
	w.bundle.Files[rel] = data
	return nil
}

// checkLimits bounds file count and cumulative size before reading, so a
// hostile or runaway repo cannot exhaust memory. It checks the size from the
// dir entry, so an oversized file is never read.
func (w *treeWalker) checkLimits(d fs.DirEntry) error {
	if len(w.bundle.Files) >= w.maxFiles {
		return fmt.Errorf("%w: too many files (limit %d)", ErrInvalidBundle, w.maxFiles)
	}
	info, err := d.Info()
	if err != nil {
		return err
	}
	if w.totalBytes+info.Size() > w.maxBytes {
		return fmt.Errorf("%w: bundle exceeds %d bytes", ErrInvalidBundle, w.maxBytes)
	}
	return nil
}

// validateBundlePath rejects empty, absolute, non-clean, backslash, and
// parent-traversal paths.
func validateBundlePath(p string) error {
	if p == "" {
		return fmt.Errorf("%w: empty path", ErrInvalidBundle)
	}
	if strings.ContainsRune(p, '\\') {
		return fmt.Errorf("%w: backslash in path %q", ErrInvalidBundle, p)
	}
	if path.IsAbs(p) {
		return fmt.Errorf("%w: absolute path %q", ErrInvalidBundle, p)
	}
	if path.Clean(p) != p {
		return fmt.Errorf("%w: non-clean path %q", ErrInvalidBundle, p)
	}
	if slices.Contains(strings.Split(p, "/"), "..") {
		return fmt.Errorf("%w: parent traversal in path %q", ErrInvalidBundle, p)
	}
	return nil
}
