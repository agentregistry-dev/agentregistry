package plugins

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/plugins/builtin"
)

// LoadEmbedded materializes every embedded plugin directory into stageDir
// (one subdir per plugin) and returns the parsed Plugin values.
//
// stageDir is typically a temp dir created by the caller; arctl shells out to
// scripts/templates inside it just like out-of-tree plugins.
func LoadEmbedded(stageDir string) ([]*Plugin, error) {
	if err := os.MkdirAll(stageDir, 0755); err != nil {
		return nil, fmt.Errorf("create stage dir: %w", err)
	}

	entries, err := fs.ReadDir(builtin.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("read embedded FS: %w", err)
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := materialize(builtin.FS, e.Name(), filepath.Join(stageDir, e.Name())); err != nil {
			return nil, fmt.Errorf("materialize embedded plugin %q: %w", e.Name(), err)
		}
	}

	plugins, err := DiscoverFromDir(stageDir)
	if err != nil {
		return nil, err
	}
	if plugins == nil {
		plugins = []*Plugin{}
	}
	return plugins, nil
}

// materialize copies subFS rooted at srcRoot to dst on disk.
func materialize(srcFS fs.FS, srcRoot, dst string) error {
	return fs.WalkDir(srcFS, srcRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(srcRoot, path)
		if err != nil {
			return err
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		data, err := fs.ReadFile(srcFS, path)
		if err != nil {
			return err
		}
		mode := os.FileMode(0644)
		// Preserve executable bit for *.sh files.
		if filepath.Ext(path) == ".sh" {
			mode = 0755
		}
		return os.WriteFile(out, data, mode)
	})
}
