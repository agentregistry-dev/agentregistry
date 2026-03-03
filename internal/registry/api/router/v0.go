// Package router contains API routing logic
package router

import (
	"net/http"

	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
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

	// Optional deployment adapters keyed by provider platform type.
	ProviderPlatforms   map[string]registrytypes.ProviderPlatformAdapter
	DeploymentPlatforms map[string]registrytypes.DeploymentPlatformAdapter

	// Optional callback for integration-owned route registration.
	ExtraRoutes func(api huma.API, pathPrefix string)
}

// RegisterRoutes registers all API routes under /v0.
func RegisterRoutes(
	api huma.API,
	cfg *config.Config,
	registry service.RegistryService,
	metrics *telemetry.Metrics,
	versionInfo *v0.VersionBody,
	opts *RouteOptions,
) {
	pathPrefix := "/v0"

	v0.RegisterHealthEndpoint(api, pathPrefix, cfg, metrics)
	v0.RegisterPingEndpoint(api, pathPrefix)
	v0.RegisterVersionEndpoint(api, pathPrefix, versionInfo)
	v0.RegisterServersEndpoints(api, pathPrefix, registry)
	v0.RegisterServersCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterEditEndpoints(api, pathPrefix, registry)
	v0auth.RegisterAuthEndpoints(api, pathPrefix, cfg)
	platformExt := v0.PlatformExtensions{}
	if opts != nil {
		platformExt.ProviderPlatforms = opts.ProviderPlatforms
		platformExt.DeploymentPlatforms = opts.DeploymentPlatforms
	}
	v0.RegisterProvidersEndpoints(api, pathPrefix, registry, platformExt)
	v0.RegisterDeploymentsEndpoints(api, pathPrefix, registry, platformExt)
	v0.RegisterAgentsEndpoints(api, pathPrefix, registry)
	v0.RegisterAgentsCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterSkillsEndpoints(api, pathPrefix, registry)
	v0.RegisterSkillsCreateEndpoint(api, pathPrefix, registry)
	v0.RegisterPromptsEndpoints(api, pathPrefix, registry)
	v0.RegisterPromptsCreateEndpoint(api, pathPrefix, registry)

	if opts != nil && opts.Indexer != nil && opts.JobManager != nil {
		v0.RegisterEmbeddingsEndpoints(api, pathPrefix, opts.Indexer, opts.JobManager)
		if opts.Mux != nil {
			v0.RegisterEmbeddingsSSEHandler(opts.Mux, pathPrefix, opts.Indexer, opts.JobManager)
		}
	}
	if opts != nil && opts.ExtraRoutes != nil {
		opts.ExtraRoutes(api, pathPrefix)
	}
}
