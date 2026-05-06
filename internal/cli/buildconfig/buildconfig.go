package buildconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Filename is the project-root file name. Filename TBD per design doc; using arctl.yaml.
const Filename = "arctl.yaml"

// Config is the per-project build-config. Future fields are additive; older
// arctl ignores unknown keys.
type Config struct {
	Framework string `yaml:"framework"`
	Language  string `yaml:"language"`
}

// Path returns the canonical arctl.yaml path under projectDir.
func Path(projectDir string) string {
	return filepath.Join(projectDir, Filename)
}

// Read parses arctl.yaml from projectDir.
func Read(projectDir string) (*Config, error) {
	data, err := os.ReadFile(Path(projectDir))
	if err != nil {
		return nil, fmt.Errorf("read arctl.yaml: %w", err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse arctl.yaml: %w", err)
	}
	if c.Framework == "" {
		return nil, fmt.Errorf("arctl.yaml: framework is required")
	}
	if c.Language == "" {
		return nil, fmt.Errorf("arctl.yaml: language is required")
	}
	return &c, nil
}

// Write serializes cfg to arctl.yaml in projectDir, overwriting any existing file.
func Write(projectDir string, cfg *Config) error {
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		return fmt.Errorf("mkdir project: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal arctl.yaml: %w", err)
	}
	return os.WriteFile(Path(projectDir), data, 0644)
}
