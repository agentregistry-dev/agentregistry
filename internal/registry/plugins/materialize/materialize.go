// Package materialize turns a plugin bundle into an on-disk harness layout. It
// is the shared hinge for deploys: given a CanonicalBundle (loaded from the
// plugin's source via the source package), translate it to the target harness's
// file set and (optionally) write that set to a directory. It owns no I/O
// beyond filesystem writes.
package materialize

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/translate"
)

// Plugin translates a canonical bundle into the target harness's on-disk file
// set. The caller loads the bundle from the plugin's resolved source (see the
// source package). The returned report records anything dropped or transformed
// for that harness.
func Plugin(b *bundle.CanonicalBundle, harness translate.Harness) (map[string][]byte, *translate.TranslationReport, error) {
	if b == nil {
		return nil, nil, fmt.Errorf("materialize: nil bundle")
	}
	files, rep, err := translate.ToHarness(harness, b)
	if err != nil {
		return nil, nil, fmt.Errorf("materialize: translate to %s: %w", harness, err)
	}
	return files, rep, nil
}

// WriteDir writes a harness file set under destDir, creating parent directories
// as needed. Paths are re-validated against traversal (defense-in-depth; the
// canonical bundle already rejects them) so a malicious path can never escape
// destDir.
func WriteDir(files map[string][]byte, destDir string) error {
	root, err := filepath.Abs(destDir)
	if err != nil {
		return err
	}
	for p, data := range files {
		if err := safeRelPath(p); err != nil {
			return err
		}
		full := filepath.Join(root, filepath.FromSlash(p))
		// Belt-and-suspenders: ensure the joined path stays within root.
		if !strings.HasPrefix(full, root+string(os.PathSeparator)) && full != root {
			return fmt.Errorf("materialize: path %q escapes destination", p)
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func safeRelPath(p string) error {
	if p == "" || strings.ContainsRune(p, '\\') || filepath.IsAbs(p) {
		return fmt.Errorf("materialize: unsafe path %q", p)
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return fmt.Errorf("materialize: path traversal in %q", p)
		}
	}
	return nil
}
