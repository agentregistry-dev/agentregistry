package plugins

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const APIVersionV1 = "arctl.dev/v1"

// Plugin is the parsed descriptor of a single framework plugin.
type Plugin struct {
	APIVersion   string  `yaml:"apiVersion"`
	Name         string  `yaml:"name"`
	Type         string  `yaml:"type"` // "agent" or "mcp"
	Framework    string  `yaml:"framework"`
	Language     string  `yaml:"language"`
	Description  string  `yaml:"description,omitempty"`
	TemplatesDir string  `yaml:"templatesDir,omitempty"`
	Env          EnvSpec `yaml:"env,omitempty"`
	Build        Command `yaml:"build,omitempty"`
	Run          Command `yaml:"run,omitempty"`

	// SourceDir is the on-disk root of this plugin (its plugin.yaml's directory).
	// Set by the loader, not in YAML.
	SourceDir string `yaml:"-"`
}

// EnvSpec advertises which env vars the plugin's runtime needs. arctl init writes
// these into .env.example; arctl run validates Required is satisfied.
type EnvSpec struct {
	Required []string `yaml:"required,omitempty"`
	Optional []string `yaml:"optional,omitempty"`
}

// Command is either an inline arg-list (preferred) or a path to a script.
type Command struct {
	Command []string `yaml:"command,omitempty"`
	Script  string   `yaml:"script,omitempty"`
}

// ParseDescriptor parses a plugin.yaml.
func ParseDescriptor(data []byte) (*Plugin, error) {
	var p Plugin
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse plugin descriptor: %w", err)
	}
	if p.APIVersion != APIVersionV1 {
		return nil, fmt.Errorf("unsupported apiVersion %q (want %q)", p.APIVersion, APIVersionV1)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("plugin: name is required")
	}
	if p.Type != "agent" && p.Type != "mcp" {
		return nil, fmt.Errorf("plugin %q: type must be \"agent\" or \"mcp\" (got %q)", p.Name, p.Type)
	}
	if p.Framework == "" {
		return nil, fmt.Errorf("plugin %q: framework is required", p.Name)
	}
	if p.Language == "" {
		return nil, fmt.Errorf("plugin %q: language is required", p.Name)
	}
	return &p, nil
}
