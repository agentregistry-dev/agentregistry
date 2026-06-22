// Package bundle is the in-memory representation of a plugin's portable core:
// a flat, path-keyed set of files (SKILL.md, AGENTS.md, .mcp.json, hooks/*,
// commands/*, agents/*, bin/*, and the real .claude-plugin/plugin.json). It is
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
	"strings"
)

// ErrInvalidBundle is returned when bundle content cannot be represented
// canonically (path traversal, backslash, absolute or non-clean path) or when
// a declared file (e.g. the manifest) is present but malformed.
var ErrInvalidBundle = errors.New("invalid plugin bundle")

// CanonicalBundle is the portable core of a plugin: a flat, path-keyed set of
// files. It is NOT harness-specific; translation to a harness's on-disk layout
// happens at deploy time.
//
// Paths are clean, relative, forward-slash separated (no leading "/" or "..").
type CanonicalBundle struct {
	Files map[string][]byte
}

// FromDir reads a checked-out plugin source tree rooted at dir into a
// CanonicalBundle. Directories, symlinks, and the .git directory are skipped;
// every regular-file path is normalized to forward slashes and
// traversal-checked. It is the bridge from a freshly-cloned source directory
// to the in-memory bundle the controller scans and deploys materialize.
func FromDir(dir string) (*CanonicalBundle, error) {
	files := map[string][]byte{}
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip symlinks (and any other irregular files) to avoid traversal out
		// of the source tree via a malicious link.
		if d.Type()&os.ModeSymlink != 0 || !d.Type().IsRegular() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if err := validateBundlePath(rel); err != nil {
			return err
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		files[rel] = data
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("%w: read source tree: %v", ErrInvalidBundle, walkErr)
	}
	return &CanonicalBundle{Files: files}, nil
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
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("%w: parent traversal in path %q", ErrInvalidBundle, p)
		}
	}
	return nil
}
