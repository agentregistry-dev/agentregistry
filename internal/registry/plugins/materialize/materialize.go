// Package materialize turns a stored plugin into an on-disk harness layout. It
// is the shared hinge between local pull and cloud deploy: pull the canonical
// bundle from the store, translate it to the target harness's file set, and
// (optionally) write that set to a directory. It owns no new I/O beyond the
// store pull and filesystem writes.
package materialize

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/store"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/translate"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Plugin pulls the canonical bundle for p from st and translates it into the
// target harness's on-disk file set. p.Spec.Content.OCIRef must be populated
// (set by the publish hook). The returned report records anything dropped or
// transformed for that harness.
func Plugin(ctx context.Context, st store.Store, p *v1alpha1.Plugin, harness translate.Harness) (map[string][]byte, *translate.TranslationReport, error) {
	if p == nil || p.Spec.Content == nil || p.Spec.Content.OCIRef == "" {
		return nil, nil, fmt.Errorf("materialize: plugin %q has no stored content (not published?)", pluginName(p))
	}
	bundle, err := st.Pull(ctx, p.Spec.Content.OCIRef)
	if err != nil {
		return nil, nil, fmt.Errorf("materialize: pull canonical bundle: %w", err)
	}
	files, rep, err := translate.ToHarness(harness, bundle)
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

func pluginName(p *v1alpha1.Plugin) string {
	if p == nil {
		return "<nil>"
	}
	return p.Metadata.Namespace + "/" + p.Metadata.Name
}
