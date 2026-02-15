// Package router contains API routing logic
package router

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	v0auth "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
)

// RouteOptions contains optional services for route registration.
type RouteOptions struct {
	Indexer    service.Indexer
	JobManager *jobs.Manager
	Mux        *http.ServeMux
}

// RegisterRoutes registers all API routes (public and admin) for all versions
// This is the single entry point for all route registration
func RegisterRoutes(
	api huma.API,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
	opts *RouteOptions,
) {
	// Public API endpoints
	registerPublicRoutes(api, "/v0", cfg, registry, metrics, versionInfo)

	// Admin API endpoints
	registerAdminRoutes(api, "/admin/v0", cfg, registry, metrics, versionInfo, opts)
}

// registerPublicRoutes registers public API routes for a version.
func registerPublicRoutes(
	api huma.API,
	pathPrefix string,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
) {
	registerCommonEndpoints(api, pathPrefix, cfg, metrics, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry)
	v0.RegisterServersCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterEditEndpoints(api, pathPrefix, registry)
	v0auth.RegisterAuthEndpoints(api, pathPrefix, cfg)
	v0.RegisterDeploymentsEndpoints(api, pathPrefix, registry)
	v0.RegisterAgentsEndpoints(api, pathPrefix, registry)
	v0.RegisterAgentsCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterSkillsEndpoints(api, pathPrefix, registry)
	v0.RegisterSkillsCreateEndpoint(api, pathPrefix, registry)
}

// registerAdminRoutes registers admin API routes for a version
func registerAdminRoutes(
	api huma.API,
	pathPrefix string,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
	opts *RouteOptions,
) {
	registerCommonEndpoints(api, pathPrefix, cfg, metrics, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry)
	v0.RegisterServersCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterEditEndpoints(api, pathPrefix, registry)
	v0.RegisterDeploymentsEndpoints(api, pathPrefix, registry)
	v0.RegisterAgentsEndpoints(api, pathPrefix, registry)
	v0.RegisterAgentsCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterSkillsEndpoints(api, pathPrefix, registry)
	v0.RegisterSkillsCreateEndpoint(api, pathPrefix, registry)

	// Register embeddings endpoints if services are available
	if opts != nil && opts.Indexer != nil && opts.JobManager != nil {
		v0.RegisterEmbeddingsEndpoints(api, pathPrefix, opts.Indexer, opts.JobManager)
		// Also register SSE handler on the mux if available
		if opts.Mux != nil {
			v0.RegisterEmbeddingsSSEHandler(opts.Mux, pathPrefix, opts.Indexer, opts.JobManager)
		}
	}
}

// registerCommonEndpoints registers endpoints that are common to both public and admin routes
func registerCommonEndpoints(
	api huma.API,
	pathPrefix string,
	cfg *config.Config,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
) {
	v0.RegisterHealthEndpoint(api, pathPrefix, cfg, metrics)
	v0.RegisterPingEndpoint(api, pathPrefix)
	v0.RegisterVersionEndpoint(api, pathPrefix, versionInfo)
}
