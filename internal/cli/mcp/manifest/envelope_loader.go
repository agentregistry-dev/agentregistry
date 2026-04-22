package manifest

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"gopkg.in/yaml.v3"
)

// loadFromEnvelope decodes declarative envelope YAML into a *ProjectManifest.
// Only fields with a direct counterpart are populated; framework-specific
// fields (Framework, Tools, Secrets, Transport, RuntimeArgs) are left at
// zero-value and filled in by kind-specific code paths where needed. The
// strict validator used by the flat-manifest path is intentionally skipped
// here because envelope files produced by `arctl init mcp` do not carry a
// Framework field. This bridge will be removed when the unified K8s-style
// API refactor rewrites the pipelines to consume v1alpha1 types directly.
func loadFromEnvelope(data []byte) (*ProjectManifest, error) {
	var doc struct {
		Metadata kinds.Metadata `yaml:"metadata"`
		Spec     kinds.MCPSpec  `yaml:"spec"`
	}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parsing envelope mcp.yaml: %w", err)
	}
	out := &ProjectManifest{
		Name:        doc.Metadata.Name,
		Version:     doc.Metadata.Version,
		Description: doc.Spec.Description,
	}
	// Extract the runtime hint from the first OCI package, if present.
	for _, p := range doc.Spec.Packages {
		if p.RegistryType == "oci" {
			out.RuntimeHint = p.RunTimeHint
			break
		}
	}
	return out, nil
}
