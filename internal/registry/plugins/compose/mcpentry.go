package compose

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ErrUnsupportedMCPServer marks an MCPServer spec shape with no faithful
// plugin-local .mcp.json representation — a TERMINAL condition for the
// referencing plugin (design: PLUGIN_COMPOSABILITY_SPIKE.md §6).
var ErrUnsupportedMCPServer = errors.New("compose: mcp server has no .mcp.json representation")

// mcpRemoteEntry is a desktop-harness remote server entry.
type mcpRemoteEntry struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

// mcpStdioEntry is a desktop-harness stdio server entry.
type mcpStdioEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// MCPEntryFromSpec maps a registry MCPServer spec to its plugin-local
// .mcp.json entry. Fidelity rules: a Remote maps directly; a Package with an
// npm/pypi origin maps to a stdio entry (explicit Launch verbatim, otherwise
// the origin-type default command); OCI package origins have no faithful
// desktop form and return ErrUnsupportedMCPServer.
func MCPEntryFromSpec(spec *v1alpha1.MCPServerSpec) (json.RawMessage, error) {
	if spec == nil {
		return nil, fmt.Errorf("%w: nil spec", ErrUnsupportedMCPServer)
	}
	if spec.Remote != nil {
		entry := mcpRemoteEntry{Type: spec.Remote.Type, URL: spec.Remote.URL}
		if len(spec.Remote.Headers) > 0 {
			entry.Headers = map[string]string{}
			for _, h := range spec.Remote.Headers {
				entry.Headers[h.Name] = h.Value
			}
		}
		return json.Marshal(entry)
	}
	if spec.Source == nil || spec.Source.Package == nil {
		return nil, fmt.Errorf("%w: neither remote nor package declared", ErrUnsupportedMCPServer)
	}
	pkg := spec.Source.Package
	entry := mcpStdioEntry{}
	switch pkg.Origin.Type {
	case v1alpha1.MCPPackageOriginTypeNPM:
		entry.Command, entry.Args = "npx", []string{"-y", pinnedIdentifier(pkg.Origin.Identifier, "@", pkg.Origin.NPM.Version)}
	case v1alpha1.MCPPackageOriginTypePyPI:
		entry.Command, entry.Args = "uvx", []string{pinnedIdentifier(pkg.Origin.Identifier, "==", pkg.Origin.PyPI.Version)}
	case v1alpha1.MCPPackageOriginTypeOCI:
		return nil, fmt.Errorf("%w: oci package origin cannot run as a plugin-local stdio server", ErrUnsupportedMCPServer)
	default:
		return nil, fmt.Errorf("%w: unknown package origin type %q", ErrUnsupportedMCPServer, pkg.Origin.Type)
	}
	// An explicit Launch owns command/args verbatim (mirrors the deploy-time
	// resolver contract on MCPPackageLaunch).
	if l := pkg.Launch; l != nil {
		if l.Command != "" {
			entry.Command = l.Command
			entry.Args = launchArgs(l.Args)
		}
		if len(l.Env) > 0 {
			entry.Env = map[string]string{}
			for _, e := range l.Env {
				entry.Env[e.Name] = e.Value
			}
		}
	}
	return json.Marshal(entry)
}

// pinnedIdentifier joins identifier and version with sep when a version is
// set, e.g. "@scope/pkg@1.2.3" or "pkg==1.2.3".
func pinnedIdentifier(identifier, sep, version string) string {
	if version == "" {
		return identifier
	}
	return identifier + sep + version
}

// launchArgs flattens MCPArguments in the same positional-then-named order
// the Kubernetes materializer uses.
func launchArgs(args []v1alpha1.MCPArgument) []string {
	var out []string
	for _, a := range args {
		if a.Type == v1alpha1.MCPArgumentTypePositional && a.Value != "" {
			out = append(out, a.Value)
		}
	}
	for _, a := range args {
		if a.Type == v1alpha1.MCPArgumentTypeNamed {
			out = append(out, a.Name)
			if a.Value != "" {
				out = append(out, a.Value)
			}
		}
	}
	return out
}
