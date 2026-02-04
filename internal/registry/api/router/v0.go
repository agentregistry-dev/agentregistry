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
	Indexer    *service.Indexer
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
	// Public API endpoints (only show published resources)
	registerPublicRoutes(api, "/v0", cfg, registry, metrics, versionInfo)
	registerPublicRoutes(api, "/v0.1", cfg, registry, metrics, versionInfo)

	// Admin API endpoints (show all resources, including unpublished)
	registerAdminRoutes(api, "/admin/v0", cfg, registry, metrics, versionInfo, opts)
	registerAdminRoutes(api, "/admin/v0.1", cfg, registry, metrics, versionInfo, opts)
}

// registerPublicRoutes registers public API routes for a version
// Public routes only return published resources
func registerPublicRoutes(
	api huma.API,
	pathPrefix string,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
) {
	// Public endpoints only show published resources
	isAdmin := false

	// Common endpoints (available in all versions)
	registerCommonEndpoints(api, pathPrefix, cfg, metrics, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry, isAdmin)
	v0.RegisterCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterEditEndpoints(api, pathPrefix, registry)
	v0auth.RegisterAuthEndpoints(api, pathPrefix, cfg)
	v0.RegisterDeploymentsEndpoints(api, pathPrefix, registry)

	// v0-only endpoints (agents and skills)
	if pathPrefix == "/v0" {
		v0.RegisterAgentsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterAgentsCreateEndpoint(api, pathPrefix, registry)
		v0.RegisterSkillsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterSkillsCreateEndpoint(api, pathPrefix, registry)
	}
}

// registerAdminRoutes registers admin API routes for a version
// Admin routes return all resources (published and unpublished)
func registerAdminRoutes(
	api huma.API,
	pathPrefix string,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
	opts *RouteOptions,
) {
	// Admin endpoints show all resources
	isAdmin := true

	// Common endpoints
	registerCommonEndpoints(api, pathPrefix, cfg, metrics, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry, isAdmin)
	v0.RegisterAdminCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterPublishStatusEndpoints(api, pathPrefix, registry)
	v0.RegisterEditEndpoints(api, pathPrefix, registry)
	v0.RegisterDeploymentsEndpoints(api, pathPrefix, registry)

	// Register embeddings endpoints if services are available
	if opts != nil && opts.Indexer != nil && opts.JobManager != nil {
		v0.RegisterEmbeddingsEndpoints(api, pathPrefix, opts.Indexer, opts.JobManager)
		// Also register SSE handler on the mux if available
		if opts.Mux != nil {
			v0.RegisterEmbeddingsSSEHandler(opts.Mux, pathPrefix, opts.Indexer, opts.JobManager)
		}
	}

	// v0-only admin endpoints (agents and skills)
	if pathPrefix == "/admin/v0" {
		v0.RegisterAgentsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterAdminAgentsCreateEndpoint(api, pathPrefix, registry)
		v0.RegisterAgentsPublishStatusEndpoints(api, pathPrefix, registry)
		v0.RegisterSkillsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterAdminSkillsCreateEndpoint(api, pathPrefix, registry)
		v0.RegisterSkillsPublishStatusEndpoints(api, pathPrefix, registry)
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
