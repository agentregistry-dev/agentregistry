package frameworks

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const APIVersionV1 = "arctl.dev/v1"

// Framework is the parsed descriptor of a single framework framework.
type Framework struct {
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

	// Launch is the default exec configuration for projects scaffolded
	// from this framework. arctl init writes it into the scaffolded
	// mcp.yaml's spec.source.package.launch when transport is stdio.
	// HTTP transport omits launch — the runtime configures the container
	// to listen on the declared port itself.
	Launch *MCPLaunch `yaml:"launch,omitempty" json:"launch,omitempty"`

	// SourceDir is the on-disk root of this framework (its framework.yaml's directory).
	// Set by the loader, not in YAML.
	SourceDir string `yaml:"-"`
}

// EnvSpec advertises which env vars the framework's runtime needs. arctl init writes
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

// MCPLaunch is the framework-level default for spec.source.package.launch
// in a scaffolded mcp.yaml. Args is a flat list of positional strings;
// arctl init expands it into the v1alpha1 structured form when it writes
// mcp.yaml. Frameworks always declare all-positional defaults at this
// layer — named args / env overrides are a per-project concern.
type MCPLaunch struct {
	Command string   `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"    json:"args,omitempty"`
}

// ToMCPArguments converts the flat-list framework defaults into the
// v1alpha1 structured form (all positional, no overrides).
func (l *MCPLaunch) ToMCPArguments() []v1alpha1.MCPArgument {
	if l == nil {
		return nil
	}
	args := make([]v1alpha1.MCPArgument, 0, len(l.Args))
	for _, a := range l.Args {
		args = append(args, v1alpha1.MCPArgument{
			Type:  v1alpha1.MCPArgumentTypePositional,
			Value: a,
		})
	}
	return args
}

// ParseDescriptor parses a framework.yaml.
func ParseDescriptor(data []byte) (*Framework, error) {
	var p Framework
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("parse framework descriptor: %w", err)
	}
	if p.APIVersion != APIVersionV1 {
		return nil, fmt.Errorf("unsupported apiVersion %q (want %q)", p.APIVersion, APIVersionV1)
	}
	if p.Name == "" {
		return nil, fmt.Errorf("framework: name is required")
	}
	if p.Type != "agent" && p.Type != "mcp" {
		return nil, fmt.Errorf("framework %q: type must be \"agent\" or \"mcp\" (got %q)", p.Name, p.Type)
	}
	if p.Framework == "" {
		return nil, fmt.Errorf("framework %q: framework is required", p.Name)
	}
	if p.Language == "" {
		return nil, fmt.Errorf("framework %q: language is required", p.Name)
	}
	return &p, nil
}
