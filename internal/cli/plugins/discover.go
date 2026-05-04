package plugins

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DiscoverFromDir scans `root` for child directories each containing a `plugin.yaml`.
// Returns parsed Plugin values (with SourceDir populated). A missing root is not an
// error — returns empty slice.
func DiscoverFromDir(root string) ([]*Plugin, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read plugin root %q: %w", root, err)
	}

	var plugins []*Plugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		descPath := filepath.Join(dir, "plugin.yaml")
		data, err := os.ReadFile(descPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", descPath, err)
		}
		p, err := ParseDescriptor(data)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: %w", e.Name(), err)
		}
		p.SourceDir = dir
		plugins = append(plugins, p)
	}
	return plugins, nil
}
