package manifest

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Manager handles loading and saving of manifest files.
type Manager[T any] struct {
	projectRoot string
	filename    string
	validator   Validator[T]
}

// NewManager creates a new manifest manager.
func NewManager[T any](projectRoot, filename string, validator Validator[T]) *Manager[T] {
	return &Manager[T]{
		projectRoot: projectRoot,
		filename:    filename,
		validator:   validator,
	}
}

// Path returns the full path to the manifest file.
func (m *Manager[T]) Path() string {
	return filepath.Join(m.projectRoot, m.filename)
}

// Exists checks if the manifest file exists.
func (m *Manager[T]) Exists() bool {
	_, err := os.Stat(m.Path())
	return err == nil
}

// Load reads and parses the manifest file.
func (m *Manager[T]) Load() (T, error) {
	var manifest T
	manifestPath := m.Path()

	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return manifest, fmt.Errorf("%s not found in %s", m.filename, m.projectRoot)
		}
		return manifest, fmt.Errorf("reading %s: %w", m.filename, err)
	}

	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return manifest, fmt.Errorf("parsing %s: %w", m.filename, err)
	}

	if m.validator != nil {
		if err := m.validator.Validate(manifest); err != nil {
			return manifest, fmt.Errorf("invalid %s: %w", m.filename, err)
		}
	}

	return manifest, nil
}

// Save writes the manifest to the file.
func (m *Manager[T]) Save(manifest T) error {
	if m.validator != nil {
		if err := m.validator.Validate(manifest); err != nil {
			return fmt.Errorf("invalid manifest: %w", err)
		}
	}

	data, err := yaml.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	if err := os.WriteFile(m.Path(), data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", m.filename, err)
	}

	return nil
}
