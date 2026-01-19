// Package router contains API routing logic
package router

import (
	"github.com/danielgtaylor/huma/v2"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	v0auth "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
)

// RegisterRoutes registers all API routes (public and admin) for all versions
// This is the single entry point for all route registration
func RegisterRoutes(
	api huma.API,
	authz auth.Authorizer,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
) {
	// Public API endpoints (only show published resources)
	registerPublicRoutes(api, "/v0", authz, cfg, registry, metrics, versionInfo)
	registerPublicRoutes(api, "/v0.1", authz, cfg, registry, metrics, versionInfo)

	// Admin API endpoints (show all resources, including unpublished)
	registerAdminRoutes(api, "/admin/v0", authz, cfg, registry, metrics, versionInfo)
	registerAdminRoutes(api, "/admin/v0.1", authz, cfg, registry, metrics, versionInfo)
}

// registerPublicRoutes registers public API routes for a version
// Public routes only return approved published resources
func registerPublicRoutes(
	api huma.API,
	pathPrefix string,
	authz auth.Authorizer,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
) {
	// Public endpoints only show approved published resources
	isAdmin := false

	// Common endpoints (available in all versions)
	registerCommonEndpoints(api, pathPrefix, cfg, metrics, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry, isAdmin)
	v0.RegisterCreateEndpoint(api, pathPrefix, registry, authz)
	v0.RegisterEditEndpoints(api, pathPrefix, registry, cfg)
	v0auth.RegisterAuthEndpoints(api, pathPrefix, cfg)
	v0.RegisterDeploymentsEndpoints(api, pathPrefix, registry)

	// v0-only endpoints (agents and skills)
	if pathPrefix == "/v0" {
		v0.RegisterAgentsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterAgentsCreateEndpoint(api, pathPrefix, registry, authz)
		v0.RegisterSkillsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterSkillsCreateEndpoint(api, pathPrefix, registry, authz)
	}
}

// registerAdminRoutes registers admin API routes for a version
// Admin routes return all resources (regardless of approval status or published status)
func registerAdminRoutes(
	api huma.API,
	pathPrefix string,
	authz auth.Authorizer,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
) {
	// Admin endpoints show all resources
	isAdmin := true

	// Common endpoints
	registerCommonEndpoints(api, pathPrefix, cfg, metrics, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry, isAdmin)
	v0.RegisterAdminCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterPublishStatusEndpoints(api, pathPrefix, registry)
	v0.RegisterEditEndpoints(api, pathPrefix, registry, cfg)
	v0.RegisterDeploymentsEndpoints(api, pathPrefix, registry)

	// v0-only admin endpoints (agents and skills)
	if pathPrefix == "/admin/v0" {
		v0.RegisterAgentsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterAdminAgentsCreateEndpoint(api, pathPrefix, registry)
		v0.RegisterAgentsPublishStatusEndpoints(api, pathPrefix, registry)
		v0.RegisterSkillsEndpoints(api, pathPrefix, registry, isAdmin)
		v0.RegisterAdminSkillsCreateEndpoint(api, pathPrefix, registry)
		v0.RegisterSkillsPublishStatusEndpoints(api, pathPrefix, registry)
		v0.RegisterAdminAgentsApprovalStatusEndpoints(api, pathPrefix, registry)
		v0.RegisterAdminServersApprovalStatusEndpoints(api, pathPrefix, registry)
		v0.RegisterAdminSkillsApprovalStatusEndpoints(api, pathPrefix, registry)
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
