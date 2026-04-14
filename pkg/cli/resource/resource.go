// Package resource provides a public API for registering declarative resource
// handlers. Enterprise or third-party extensions can implement ResourceHandler
// and call Register to extend the arctl declarative CLI (apply/get/delete).
package resource

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/resource"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
)

// ResourceHandler is the public alias for the internal ResourceHandler interface.
// Implementing this interface and calling Register makes a resource type
// available to `arctl apply`, `arctl get`, and `arctl delete`.
type ResourceHandler = resource.ResourceHandler

// Metadata is the public alias for scheme.Metadata.
type Metadata = scheme.Metadata

// Resource is the public alias for scheme.Resource.
type Resource = scheme.Resource

// Client is the public alias for the OSS registry client.
// Handlers that only use the enterprise client can safely ignore this parameter.
type Client = client.Client

// APIVersion is the canonical apiVersion string for arctl resources ("ar.dev/v1alpha1").
const APIVersion = scheme.APIVersion

// Register adds h to the global resource registry used by arctl declarative commands.
// Call this from an init() function or before cli.Root() is invoked.
func Register(h ResourceHandler) {
	resource.Register(h)
}

// Lookup resolves a type name (kind, singular, or plural) to its ResourceHandler.
func Lookup(name string) (ResourceHandler, error) {
	return resource.Lookup(name)
}
