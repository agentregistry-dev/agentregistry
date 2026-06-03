package v1alpha1

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// MCPServer is the typed envelope for kind=MCPServer resources.
type MCPServer struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta    `json:"metadata" yaml:"metadata"`
	Spec     MCPServerSpec `json:"spec" yaml:"spec"`
	Status   Status        `json:"status,omitzero" yaml:"status,omitempty"`
}

// MCPServerSpec is the MCP server's declarative body.
type MCPServerSpec struct {
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Source declares where the bundled MCP server comes from — Package (the
	// runnable distribution) and/or Repository (the source code).
	Source *MCPServerSource `json:"source,omitempty" yaml:"source,omitempty"`

	// Remote declares a remote MCP server instead of a bundled one. These are pre-existing
	// MCP servers that the registry does not deploy but can be referenced by Agents.
	Remote *MCPRemote `json:"remote,omitempty" yaml:"remote,omitempty"`
}

// MCPRemote describes a pre-running remote MCP server that the registry
// does not deploy. Distinct from MCPTransport (used inside MCPPackage to
// describe a deployable package's transport) because remote headers carry
// only static name/value pairs - no templating.
type MCPRemote struct {
	Type    string       `json:"type" yaml:"type"`
	URL     string       `json:"url" yaml:"url"`
	Headers []HTTPHeader `json:"headers,omitempty" yaml:"headers,omitempty"`
}

// HTTPHeader is an HTTP header sent on requests to a remote MCP server.
type HTTPHeader struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value,omitempty" yaml:"value,omitempty"`
}

// MCPServerSource is the distribution origin of a bundled MCP server —
// either a published artifact (Package) or a source repository the
// registry builds from.
type MCPServerSource struct {
	// Package is the runnable distribution (stdio binary, container image,
	// npm package, etc.) of this MCP server.
	Package *MCPPackage `json:"package,omitempty" yaml:"package,omitempty"`

	// Repository links to the source code the package was built from.
	Repository *Repository `json:"repository,omitempty" yaml:"repository,omitempty"`
}

// MCPTransport describes how a deployable MCPPackage exposes itself. Used
// only inside MCPPackage; remotes use MCPRemote, which carries its own URL.
// For http, the listen Port and endpoint Path are set explicitly because the
// host is constructed at deploy time. Both are ignored for stdio.
type MCPTransport struct {
	Type string `json:"type" yaml:"type"`                     // "http" | "stdio"
	Port uint16 `json:"port,omitempty" yaml:"port,omitempty"` // http listen port 1-65535 (ignored for stdio)
	Path string `json:"path,omitempty" yaml:"path,omitempty"` // http endpoint path, e.g. "/mcp" (ignored for stdio)
}

// MCPPackage is a runnable distribution of an MCP server, grouped by
// concern: Origin (what to fetch), Launch (how to start it), Transport
// (how to talk to it).
type MCPPackage struct {
	Origin    MCPPackageOrigin  `json:"origin" yaml:"origin"`
	Launch    *MCPPackageLaunch `json:"launch,omitempty" yaml:"launch,omitempty"`
	Transport MCPTransport      `json:"transport" yaml:"transport"`
}

// MCPPackageOrigin identifies the package and where to fetch it. The Type
// discriminator selects which per-type sub-struct must be set; exactly one
// of NPM/PyPI/OCI is non-nil, matching Type.
type MCPPackageOrigin struct {
	Type       MCPPackageOriginType `json:"type" yaml:"type"`
	Identifier string               `json:"identifier" yaml:"identifier"`

	NPM  *MCPPackageOriginNPM  `json:"npm,omitempty"  yaml:"npm,omitempty"`
	PyPI *MCPPackageOriginPyPI `json:"pypi,omitempty" yaml:"pypi,omitempty"`
	OCI  *MCPPackageOriginOCI  `json:"oci,omitempty"  yaml:"oci,omitempty"`
}

type MCPPackageOriginType string

const (
	MCPPackageOriginTypeNPM  MCPPackageOriginType = "npm"
	MCPPackageOriginTypePyPI MCPPackageOriginType = "pypi"
	MCPPackageOriginTypeOCI  MCPPackageOriginType = "oci"
)

// MCPPackageOriginNPM holds npm-specific fetch inputs.
type MCPPackageOriginNPM struct {
	Version    string `json:"version" yaml:"version"`
	Mirror     string `json:"mirror,omitempty" yaml:"mirror,omitempty"`
	ServerName string `json:"serverName" yaml:"serverName"`
}

// MCPPackageOriginPyPI holds pypi-specific fetch inputs.
type MCPPackageOriginPyPI struct {
	Version    string `json:"version" yaml:"version"`
	Mirror     string `json:"mirror,omitempty" yaml:"mirror,omitempty"`
	ServerName string `json:"serverName" yaml:"serverName"`
}

// MCPPackageOriginOCI holds oci-specific fetch inputs. Version is encoded
// in Identifier (e.g. "ghcr.io/foo/bar:1.0.0" or "...@sha256:..."); bare
// image refs that would silently resolve `:latest` are rejected by the
// validator.
type MCPPackageOriginOCI struct {
	ServerName string `json:"serverName" yaml:"serverName"`
}

// MCPPackageLaunch declares how to start the fetched package. If Launch
// is nil, the resolver derives Command and Args from Origin.Type defaults
// (npm → "npx -y <id>@<ver>"; pypi → "uvx <id>==<ver>"; oci → image
// entrypoint). If Launch is set, the manifest owns Command and Args
// verbatim — no implicit identifier injection. Command may be empty only
// for oci.
type MCPPackageLaunch struct {
	Command string             `json:"command,omitempty" yaml:"command,omitempty"`
	Args    []MCPArgument      `json:"args,omitempty" yaml:"args,omitempty"`
	Env     []MCPKeyValueInput `json:"env,omitempty" yaml:"env,omitempty"`
}

// UnmarshalYAML accepts two forms for the `args` key.
//
// Form 1 — structured (canonical, also the form JSON consumers see and
// the form the marshaller emits):
//
//	args:
//	  - type: positional
//	    value: src/main.py
//	  - type: named
//	    name: --port
//	    value: "3000"
//
// Form 2 — flat-list shorthand (YAML-only, for hand-written manifests
// where every argument is positional):
//
//	args: [src/main.py, "--debug"]
//
// Each string element of the shorthand becomes a positional MCPArgument
// with Value set to the string. Mixing string scalars with mapping
// elements in the same list is rejected with a clear error.
//
// On marshalling, MCPPackageLaunch always emits the structured form;
// the shorthand is an input-only convenience.
func (l *MCPPackageLaunch) UnmarshalYAML(value *yaml.Node) error {
	// Decode into an intermediate where Args is a raw node so we can
	// inspect its shape before dispatching to either branch.
	type raw struct {
		Command string             `yaml:"command,omitempty"`
		Args    yaml.Node          `yaml:"args,omitempty"`
		Env     []MCPKeyValueInput `yaml:"env,omitempty"`
	}
	var r raw
	if err := value.Decode(&r); err != nil {
		return err
	}
	l.Command = r.Command
	l.Env = r.Env

	// Zero node (no `args:` key at all) → leave Args nil.
	if r.Args.Kind == 0 {
		l.Args = nil
		return nil
	}
	if r.Args.Kind != yaml.SequenceNode {
		return fmt.Errorf("launch.args: expected a sequence, got %s", nodeKindName(r.Args.Kind))
	}
	// Empty list → leave Args nil (honors omitempty).
	if len(r.Args.Content) == 0 {
		l.Args = nil
		return nil
	}

	switch r.Args.Content[0].Kind {
	case yaml.ScalarNode:
		// Flat-list shorthand: every element must be a string scalar.
		args := make([]MCPArgument, 0, len(r.Args.Content))
		for i, n := range r.Args.Content {
			if n.Kind != yaml.ScalarNode {
				return fmt.Errorf("launch.args[%d]: flat-list shorthand requires all elements to be string scalars; found %s", i, nodeKindName(n.Kind))
			}
			var s string
			if err := n.Decode(&s); err != nil {
				return fmt.Errorf("launch.args[%d]: %w", i, err)
			}
			args = append(args, MCPArgument{
				Type:  MCPArgumentTypePositional,
				Value: s,
			})
		}
		l.Args = args
		return nil
	case yaml.MappingNode:
		// Structured form: decode each element as MCPArgument.
		args := make([]MCPArgument, 0, len(r.Args.Content))
		for i, n := range r.Args.Content {
			if n.Kind != yaml.MappingNode {
				return fmt.Errorf("launch.args[%d]: structured form requires all elements to be mappings; found %s", i, nodeKindName(n.Kind))
			}
			var a MCPArgument
			if err := n.Decode(&a); err != nil {
				return fmt.Errorf("launch.args[%d]: %w", i, err)
			}
			args = append(args, a)
		}
		l.Args = args
		return nil
	default:
		return fmt.Errorf("launch.args[0]: expected a string scalar or a mapping, got %s", nodeKindName(r.Args.Content[0].Kind))
	}
}

// nodeKindName returns a short label for a yaml.Node kind, used in error
// messages so users see "mapping" / "sequence" rather than raw integers.
func nodeKindName(k yaml.Kind) string {
	switch k {
	case yaml.DocumentNode:
		return "document"
	case yaml.SequenceNode:
		return "sequence"
	case yaml.MappingNode:
		return "mapping"
	case yaml.ScalarNode:
		return "scalar"
	case yaml.AliasNode:
		return "alias"
	default:
		return fmt.Sprintf("kind(%d)", k)
	}
}

// MCPArgument is one command-line argument.
type MCPArgument struct {
	Type  MCPArgumentType `json:"type" yaml:"type"`
	Name  string          `json:"name,omitempty" yaml:"name,omitempty"`
	Value string          `json:"value,omitempty" yaml:"value,omitempty"`
}

type MCPArgumentType string

const (
	MCPArgumentTypePositional MCPArgumentType = "positional"
	MCPArgumentTypeNamed      MCPArgumentType = "named"
)

// MCPKeyValueInput is one declared environment variable.
type MCPKeyValueInput struct {
	Name       string `json:"name" yaml:"name"`
	Value      string `json:"value,omitempty" yaml:"value,omitempty"`
	IsRequired bool   `json:"isRequired,omitempty" yaml:"isRequired,omitempty"`
}
