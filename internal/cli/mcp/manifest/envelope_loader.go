package manifest

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// loadFromEnvelope decodes declarative envelope YAML into a *ProjectManifest.
// Only fields with a direct counterpart are populated; framework-specific
// fields (Framework, Tools, Secrets, Transport, RuntimeArgs) are left at
// zero-value and filled in by kind-specific code paths where needed. The
// strict validator used by the flat-manifest path is intentionally skipped
// here because envelope files produced by `arctl init mcp` do not carry a
// Framework field.
func loadFromEnvelope(data []byte) (*ProjectManifest, error) {
	var server v1alpha1.MCPServer
	if err := v1alpha1.Default.DecodeInto(data, &server); err != nil {
		return nil, fmt.Errorf("parsing envelope mcp.yaml: %w", err)
	}
	out := &ProjectManifest{
		Name:        server.Metadata.Name,
		Version:     server.Metadata.Version,
		Description: server.Spec.Description,
	}
	// Extract the runtime hint from the first OCI package, if present.
	for _, p := range server.Spec.Packages {
		if p.RegistryType == "oci" {
			out.RuntimeHint = p.RuntimeHint
			break
		}
	}
	return out, nil
}
