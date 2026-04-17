// Package resource is retained for import-path compatibility.
// The internal/cli/resource package has been deleted (Task 19).
// Enterprise extensions should use pkg/cli/declarative to register kinds via kinds.Kind.
package resource

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
)

// Metadata is the public alias for scheme.Metadata.
type Metadata = scheme.Metadata

// Resource is the public alias for scheme.Resource.
type Resource = scheme.Resource

// APIVersion is the canonical apiVersion string for arctl resources ("ar.dev/v1alpha1").
const APIVersion = scheme.APIVersion
