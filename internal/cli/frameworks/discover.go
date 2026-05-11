package frameworks

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DiscoverFromDir scans `root` for child directories each containing a `framework.yaml`.
// Returns parsed Framework values (with SourceDir populated). A missing root is not an
// error — returns empty slice.
func DiscoverFromDir(root string) ([]*Framework, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read framework root %q: %w", root, err)
	}

	var frameworks []*Framework
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		descPath := filepath.Join(dir, "framework.yaml")
		data, err := os.ReadFile(descPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read %s: %w", descPath, err)
		}
		p, err := ParseDescriptor(data)
		if err != nil {
			return nil, fmt.Errorf("framework %q: %w", e.Name(), err)
		}
		p.SourceDir = dir
		frameworks = append(frameworks, p)
	}
	return frameworks, nil
}

// UserFrameworksDir returns the user-level framework directory.
// Honors XDG_CONFIG_HOME; falls back to ~/.config/arctl/frameworks.
func UserFrameworksDir() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "arctl", "frameworks")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "arctl", "frameworks")
}

// ProjectFrameworksDir returns the project-local framework directory under projectRoot.
func ProjectFrameworksDir(projectRoot string) string {
	return filepath.Join(projectRoot, ".arctl", "frameworks")
}
