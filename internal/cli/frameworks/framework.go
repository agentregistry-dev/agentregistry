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
	// LocalInstall runs before stdio `arctl run`. nil = no preflight.
	LocalInstall *Command `yaml:"localInstall,omitempty"`

	// Launch declares per-transport exec defaults for projects scaffolded
	// from this framework. arctl init writes the block matching the
	// requested --transport into the scaffolded mcp.yaml's
	// spec.source.package.launch.
	Launch *FrameworkLaunch `yaml:"launch,omitempty" json:"launch,omitempty"`

	// LocalLaunch overrides Launch for `arctl run` only. nil falls back to Launch.
	LocalLaunch *FrameworkLaunch `yaml:"localLaunch,omitempty" json:"localLaunch,omitempty"`

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

// FrameworkLaunch declares the per-transport exec defaults a framework
// scaffolds into mcp.yaml. Each transport (stdio/http) is optional; a
// framework that only supports one transport may declare just that one.
// The block matching the user's --transport flag is selected at init.
type FrameworkLaunch struct {
	Stdio *MCPLaunch `yaml:"stdio,omitempty" json:"stdio,omitempty"`
	HTTP  *MCPLaunch `yaml:"http,omitempty"  json:"http,omitempty"`
}

// ForTransport returns the launch defaults for the given transport, or
// nil if the framework didn't declare one. Callers fall back to writing
// no Launch when this is nil.
func (l *FrameworkLaunch) ForTransport(transport string) *MCPLaunch {
	if l == nil {
		return nil
	}
	switch transport {
	case "stdio":
		return l.Stdio
	case "http":
		return l.HTTP
	default:
		return nil
	}
}

// MCPLaunch is one transport's default for spec.source.package.launch
// in a scaffolded mcp.yaml. Args is a flat list of positional strings;
// arctl init expands it into the v1alpha1 structured form when it writes
// mcp.yaml. Args support Go text/template substitution; the supported
// variable is {{.Port}} (the user's --port flag). Frameworks always
// declare all-positional defaults at this layer — named args / env
// overrides are a per-project concern.
type MCPLaunch struct {
	Command string   `yaml:"command,omitempty" json:"command,omitempty"`
	Args    []string `yaml:"args,omitempty"    json:"args,omitempty"`
}

// Render substitutes template variables in Args. The single supported
// variable today is {{.Port}}, matching the existing pattern used by
// build.command and run.command. Command is returned verbatim.
func (l *MCPLaunch) Render(port int) (string, []string, error) {
	if l == nil {
		return "", nil, nil
	}
	args, err := RenderArgs(l.Args, map[string]any{"Port": port})
	if err != nil {
		return "", nil, err
	}
	return l.Command, args, nil
}

// ToMCPArguments converts a flat string list to the v1alpha1 structured
// form (all positional, no overrides). Used by arctl init after Render.
func ToMCPArguments(args []string) []v1alpha1.MCPArgument {
	out := make([]v1alpha1.MCPArgument, 0, len(args))
	for _, a := range args {
		out = append(out, v1alpha1.MCPArgument{
			Type:  v1alpha1.MCPArgumentTypePositional,
			Value: a,
		})
	}
	return out
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
	if p.Launch != nil && p.Launch.Stdio == nil && p.Launch.HTTP == nil {
		return nil, fmt.Errorf("framework %q: launch must declare at least one of launch.stdio or launch.http "+
			"(the legacy single-block shape `launch: {command, args}` is no longer supported)", p.Name)
	}
	if p.LocalLaunch != nil && p.LocalLaunch.Stdio == nil && p.LocalLaunch.HTTP == nil {
		return nil, fmt.Errorf("framework %q: localLaunch must declare at least one of localLaunch.stdio or localLaunch.http "+
			"(omit the localLaunch block entirely to fall back to launch)", p.Name)
	}
	return &p, nil
}
