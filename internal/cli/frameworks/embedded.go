package frameworks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/frameworks/builtin"
)

// LoadEmbedded materializes every embedded framework directory into stageDir
// (one subdir per framework) and returns the parsed Framework values.
//
// stageDir is typically a temp dir created by the caller; arctl shells out to
// scripts/templates inside it just like out-of-tree frameworks.
func LoadEmbedded(stageDir string) ([]*Framework, error) {
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
			return nil, fmt.Errorf("materialize embedded framework %q: %w", e.Name(), err)
		}
	}

	frameworks, err := DiscoverFromDir(stageDir)
	if err != nil {
		return nil, err
	}
	if frameworks == nil {
		frameworks = []*Framework{}
	}
	return frameworks, nil
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
