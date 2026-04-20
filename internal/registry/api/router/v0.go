// Package router contains API routing logic
package router

import (
	"net/http"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"

	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	v0agents "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/agents"
	v0deployments "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deployments"
	v0embeddings "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/embeddings"
	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	v0ping "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/ping"
	v0prompts "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/prompts"
	v0providers "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/providers"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/resource"
	v0servers "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/servers"
	v0skills "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/skills"
	v0version "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/version"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	promptsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/prompt"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	skillsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/skill"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
)

// RegistryServices bundles all per-domain service registries for route registration.
type RegistryServices struct {
	Server     serversvc.Registry
	Agent      agentsvc.Registry
	Skill      skillsvc.Registry
	Prompt     promptsvc.Registry
	Provider   providersvc.Registry
	Deployment deploymentsvc.Registry
}

// V1Alpha1Stores is the per-kind Store map used by the v1alpha1
// resource handler, keyed by v1alpha1 Kind name (e.g. "Agent",
// "MCPServer"). Produced by database.NewV1Alpha1Stores; enterprise
// builds may extend the map with additional kinds before passing it
// in.
//
// When non-nil on RouteOptions, RegisterRoutes wires the v1alpha1
// generic handler at `/v0/namespaces/{ns}/{plural}/...` alongside the
// legacy per-kind handlers at the unnamespaced `/v0/{plural}/...`
// paths.
type V1Alpha1Stores = map[string]*internaldb.Store

// RouteOptions contains optional services for route registration.
type RouteOptions struct {
	Indexer    service.Indexer
	JobManager *jobs.Manager
	Mux        *http.ServeMux

	// Optional deployment adapters keyed by provider platform type.
	ProviderPlatforms   map[string]registrytypes.ProviderPlatformAdapter
	DeploymentPlatforms map[string]registrytypes.DeploymentPlatformAdapter

	// V1Alpha1Stores, when non-empty, enables the generic v1alpha1 handler
	// at `/v0/namespaces/{ns}/{plural}/...`. Legacy routes stay live
	// regardless; clients migrate off them incrementally.
	V1Alpha1Stores V1Alpha1Stores

	// V1Alpha1Importer, when non-nil, enables POST /v0/import. Typically
	// constructed alongside V1Alpha1Stores at bootstrap with the OSS
	// scanner set (OSV + Scorecard) + a FindingsStore bound to the
	// same pool.
	V1Alpha1Importer *importer.Importer

	// Optional callback for integration-owned route registration.
	ExtraRoutes func(api huma.API, pathPrefix string)
}

// RegisterRoutes registers all API routes under /v0.
func RegisterRoutes(
	api huma.API,
	cfg *config.Config,
	svcs RegistryServices,
	metrics *telemetry.Metrics,
	versionInfo *apitypes.VersionBody,
	opts *RouteOptions,
) {
	pathPrefix := "/v0"

	v0health.RegisterHealthEndpoint(api, pathPrefix, cfg, metrics)
	v0ping.RegisterPingEndpoint(api, pathPrefix)
	v0version.RegisterVersionEndpoint(api, pathPrefix, versionInfo)
	v0servers.RegisterServersEndpoints(api, pathPrefix, svcs.Server, svcs.Deployment)
	v0servers.RegisterServersCreateEndpoint(api, pathPrefix, svcs.Server, svcs.Deployment)
	v0servers.RegisterEditEndpoints(api, pathPrefix, svcs.Server, svcs.Deployment)
	v0providers.RegisterProvidersEndpoints(api, pathPrefix, svcs.Provider)
	v0deployments.RegisterDeploymentsEndpoints(api, pathPrefix, svcs.Deployment)
	v0agents.RegisterAgentsEndpoints(api, pathPrefix, svcs.Agent, svcs.Deployment)
	v0agents.RegisterAgentsCreateEndpoint(api, pathPrefix, svcs.Agent, svcs.Deployment)
	v0skills.RegisterSkillsEndpoints(api, pathPrefix, svcs.Skill)
	v0skills.RegisterSkillsCreateEndpoint(api, pathPrefix, svcs.Skill)
	v0prompts.RegisterPromptsEndpoints(api, pathPrefix, svcs.Prompt)
	v0prompts.RegisterPromptsCreateEndpoint(api, pathPrefix, svcs.Prompt)

	if opts != nil && opts.Indexer != nil && opts.JobManager != nil {
		v0embeddings.RegisterEmbeddingsEndpoints(api, pathPrefix, opts.Indexer, opts.JobManager)
		if opts.Mux != nil {
			v0embeddings.RegisterEmbeddingsSSEHandler(opts.Mux, pathPrefix, opts.Indexer, opts.JobManager)
		}
	}

	// v1alpha1 generic routes — wired when V1Alpha1Stores is provided.
	// Lives alongside the legacy per-kind routes; clients migrate over
	// time. Cross-kind dangling-ref detection uses a Store-backed
	// resolver.
	if opts != nil && len(opts.V1Alpha1Stores) > 0 {
		registerV1Alpha1Routes(api, pathPrefix, opts.V1Alpha1Stores)
	}

	// POST /v0/import — runs decoded manifests through the enrichment
	// pipeline (validate + scanners + findings-write) before Upsert.
	// Independent of V1Alpha1Stores wiring because enterprise builds
	// may provide their own Importer.
	if opts != nil && opts.V1Alpha1Importer != nil {
		resource.RegisterImport(api, resource.ImportConfig{
			BasePrefix: pathPrefix,
			Importer:   opts.V1Alpha1Importer,
		})
	}

	if opts != nil && opts.ExtraRoutes != nil {
		opts.ExtraRoutes(api, pathPrefix)
	}
}

// registerV1Alpha1Routes wires the generic resource handler for every
// built-in kind at `{basePrefix}/namespaces/{ns}/{plural}/...` plus
// cross-namespace list at `{basePrefix}/{plural}`, and the multi-doc
// apply endpoint at `{basePrefix}/apply`. Cross-kind ResourceRef
// existence dispatches through the shared
// internaldb.NewV1Alpha1Resolver so the router and any server-side
// Importer both see the same ref-existence semantics.
func registerV1Alpha1Routes(api huma.API, basePrefix string, stores V1Alpha1Stores) {
	resolver := internaldb.NewV1Alpha1Resolver(stores)
	registryValidator := registries.Dispatcher
	uniqueRemoteURLs := internaldb.NewV1Alpha1UniqueRemoteURLsChecker(stores)

	// Per-kind CRUD endpoints — one call per built-in kind, hidden
	// inside resource.RegisterBuiltins.
	resource.RegisterBuiltins(api, basePrefix, stores, resolver, registryValidator, uniqueRemoteURLs)

	// Multi-doc YAML batch apply at POST {basePrefix}/apply. Shares
	// the same Stores map + Resolver + RegistryValidator + uniqueness
	// checker — no second per-kind table here.
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix:              basePrefix,
		Stores:                  stores,
		Resolver:                resolver,
		RegistryValidator:       registryValidator,
		UniqueRemoteURLsChecker: uniqueRemoteURLs,
	})
}
