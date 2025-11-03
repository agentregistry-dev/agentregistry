// Package router contains API routing logic
package router

import (
	"github.com/danielgtaylor/huma/v2"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
)

// RegisterAPIRoutes registers general API routes (non-registry specific)
func RegisterAPIRoutes(api huma.API, versionInfo *v0.VersionBody) {
	// TODO: non-registry specific API routes
}
