// Package scheme is the thin CLI-side wrapper over kinds.Registry decoding.
// It exists to keep CLI code decoupled from the registry package's fully-qualified
// type names when reading YAML files.
package scheme

import (
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// APIVersion is the canonical apiVersion string for arctl declarative YAML files.
// This re-exports models.APIVersion so CLI code does not need to import pkg/models directly.
const APIVersion = models.APIVersion

// Resource is an alias for kinds.Document. All CLI code should read doc.Spec via a
// typed assertion against the per-kind Spec struct (e.g. *agent.Spec).
type Resource = kinds.Document

// Metadata re-exports kinds.Metadata for CLI callers.
type Metadata = kinds.Metadata

// DecodeBytes parses one or more YAML documents using the provided registry.
// Returns an error if any document has an unknown kind or an unparseable spec.
func DecodeBytes(reg *kinds.Registry, b []byte) ([]*Resource, error) {
	return reg.DecodeMulti(b)
}

// DecodeFile reads a YAML file and decodes it using the provided registry.
func DecodeFile(reg *kinds.Registry, path string) ([]*Resource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(reg, b)
}
