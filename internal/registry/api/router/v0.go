// Package router contains API routing logic
package router

import (
	"net/http"

	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	v0agents "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/agents"
	v0deployments "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deployments"
	v0extensions "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/extensions"
	v0prompts "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/prompts"
	v0providers "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/providers"
	v0servers "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/servers"
	v0skills "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/skills"
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
	serverSvc v0servers.ServerService,
	agentSvc v0agents.AgentService,
	skillSvc v0skills.SkillService,
	promptSvc v0prompts.PromptService,
	providerSvc v0providers.ProviderService,
	deploymentSvc v0deployments.DeploymentService,
	metrics *telemetry.Metrics,
	versionInfo *apitypes.VersionBody,
	opts *RouteOptions,
) {
	pathPrefix := "/v0"

	v0.RegisterHealthEndpoint(api, pathPrefix, cfg, metrics)
	v0.RegisterPingEndpoint(api, pathPrefix)
	v0.RegisterVersionEndpoint(api, pathPrefix, versionInfo)
	v0servers.RegisterServersEndpoints(api, pathPrefix, serverSvc, deploymentSvc)
	v0servers.RegisterServersCreateEndpoint(api, pathPrefix, serverSvc, deploymentSvc)
	v0servers.RegisterEditEndpoints(api, pathPrefix, serverSvc, deploymentSvc)
	v0auth.RegisterAuthEndpoints(api, pathPrefix, cfg)
	platformExt := v0extensions.PlatformExtensions{}
	if opts != nil {
		platformExt.ProviderPlatforms = opts.ProviderPlatforms
		platformExt.DeploymentPlatforms = opts.DeploymentPlatforms
	}
	v0providers.RegisterProvidersEndpoints(api, pathPrefix, providerSvc, platformExt)
	v0deployments.RegisterDeploymentsEndpoints(api, pathPrefix, providerSvc, deploymentSvc, platformExt)
	v0agents.RegisterAgentsEndpoints(api, pathPrefix, agentSvc, deploymentSvc)
	v0agents.RegisterAgentsCreateEndpoint(api, pathPrefix, agentSvc, deploymentSvc)
	v0skills.RegisterSkillsEndpoints(api, pathPrefix, skillSvc)
	v0skills.RegisterSkillsCreateEndpoint(api, pathPrefix, skillSvc)
	v0prompts.RegisterPromptsEndpoints(api, pathPrefix, promptSvc)
	v0prompts.RegisterPromptsCreateEndpoint(api, pathPrefix, promptSvc)

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
