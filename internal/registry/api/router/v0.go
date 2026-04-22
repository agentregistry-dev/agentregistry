// Package router contains API routing logic
package router

import (
	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	v0embeddings "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/embeddings"
	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	v0ping "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/ping"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/resource"
	v0version "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/version"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
)

// V1Alpha1Stores is the per-kind Store map used by the v1alpha1
// resource handler, keyed by v1alpha1 Kind name (e.g. "Agent",
// "MCPServer"). Produced by database.NewV1Alpha1Stores; enterprise
// builds may extend the map with additional kinds before passing it
// in.
type V1Alpha1Stores = map[string]*internaldb.Store

// RouteOptions contains optional services for route registration.
type RouteOptions struct {
	// ProviderPlatforms registers platform-side provider adapters keyed by
	// platform string (enterprise extension point).
	ProviderPlatforms map[string]registrytypes.ProviderPlatformAdapter

	// V1Alpha1Stores, when non-empty, enables the generic v1alpha1 handler
	// at `/v0/namespaces/{ns}/{plural}/...`.
	V1Alpha1Stores V1Alpha1Stores

	// V1Alpha1Importer, when non-nil, enables POST /v0/import. Typically
	// constructed alongside V1Alpha1Stores at bootstrap with the OSS
	// scanner set (OSV + Scorecard) + a FindingsStore bound to the
	// same pool.
	V1Alpha1Importer *importer.Importer

	// V1Alpha1DeploymentCoordinator drives post-persist reconciliation
	// for the Deployment kind: PUT → adapter.Apply; DELETE → adapter.Remove.
	// Constructed alongside V1Alpha1Stores at bootstrap, wired into the
	// generic resource handler via resource.DeploymentHooks. Nil disables
	// Deployment reconciliation — PUT still persists the row, DELETE
	// still soft-deletes, but no adapter dispatch happens.
	V1Alpha1DeploymentCoordinator *deploymentsvc.V1Alpha1Coordinator

	// V1Alpha1Indexer + V1Alpha1JobManager, when both non-nil, enable
	// POST /v0/embeddings/index + GET /v0/embeddings/index/{jobId}.
	// Constructed at bootstrap only when AGENT_REGISTRY_EMBEDDINGS_ENABLED
	// is set and a provider is reachable; nil disables the endpoints.
	V1Alpha1Indexer    *embeddings.Indexer
	V1Alpha1JobManager *jobs.Manager

	// V1Alpha1SemanticSearch, when non-nil, enables
	// `?semantic=<q>&semanticThreshold=<f>` on list endpoints. The
	// func embeds the query string and returns the vector; the list
	// handler then routes through Store.SemanticList.
	V1Alpha1SemanticSearch resource.SemanticSearchFunc

	// Optional callback for integration-owned route registration.
	ExtraRoutes func(api huma.API, pathPrefix string)
}

// RegisterRoutes registers all API routes under /v0.
func RegisterRoutes(
	api huma.API,
	cfg *config.Config,
	metrics *telemetry.Metrics,
	versionInfo *apitypes.VersionBody,
	opts *RouteOptions,
) {
	pathPrefix := "/v0"

	v0health.RegisterHealthEndpoint(api, pathPrefix, cfg, metrics)
	v0ping.RegisterPingEndpoint(api, pathPrefix)
	v0version.RegisterVersionEndpoint(api, pathPrefix, versionInfo)

	if opts == nil {
		return
	}

	// v1alpha1 generic routes — wired when V1Alpha1Stores is provided.
	// Cross-kind dangling-ref detection uses a Store-backed resolver.
	// Deployment reconciliation hooks plug in when the coordinator is
	// supplied.
	if len(opts.V1Alpha1Stores) > 0 {
		registerV1Alpha1Routes(api, pathPrefix, opts.V1Alpha1Stores, opts.V1Alpha1DeploymentCoordinator, opts.V1Alpha1SemanticSearch)
	}

	// POST /v0/import — runs decoded manifests through the enrichment
	// pipeline (validate + scanners + findings-write) before Upsert.
	if opts.V1Alpha1Importer != nil {
		resource.RegisterImport(api, resource.ImportConfig{
			BasePrefix: pathPrefix,
			Importer:   opts.V1Alpha1Importer,
		})
	}

	// Embeddings indexer endpoints — wired only when both the indexer
	// and job manager are present.
	if opts.V1Alpha1Indexer != nil && opts.V1Alpha1JobManager != nil {
		v0embeddings.Register(api, v0embeddings.Config{
			BasePrefix: pathPrefix,
			Indexer:    opts.V1Alpha1Indexer,
			Manager:    opts.V1Alpha1JobManager,
		})
	}

	if opts.ExtraRoutes != nil {
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
//
// When coord is non-nil, Deployment PUT/DELETE fire
// coord.Apply/coord.Remove after the row is persisted so the platform
// adapter converges runtime state synchronously with the API call.
func registerV1Alpha1Routes(api huma.API, basePrefix string, stores V1Alpha1Stores, coord *deploymentsvc.V1Alpha1Coordinator, semantic resource.SemanticSearchFunc) {
	resolver := internaldb.NewV1Alpha1Resolver(stores)
	registryValidator := registries.Dispatcher
	uniqueRemoteURLs := internaldb.NewV1Alpha1UniqueRemoteURLsChecker(stores)

	hooks := resource.DeploymentHooks{}
	if coord != nil {
		hooks.PostUpsert = coord.Apply
		hooks.PostDelete = coord.Remove
	}

	// Per-kind CRUD endpoints — one call per built-in kind, hidden
	// inside resource.RegisterBuiltins.
	resource.RegisterBuiltins(api, basePrefix, stores, resolver, registryValidator, uniqueRemoteURLs, hooks, semantic)

	// Deployment-specific endpoints: logs stream (cancel is subsumed
	// by DesiredState=undeployed + DELETE in the v1alpha1 lifecycle).
	if coord != nil {
		resource.RegisterDeploymentLogs(api, resource.DeploymentLogsConfig{
			BasePrefix:  basePrefix,
			Store:       stores[v1alpha1.KindDeployment],
			Coordinator: coord,
		})
	}

	// Multi-doc YAML batch apply at POST {basePrefix}/apply.
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix:              basePrefix,
		Stores:                  stores,
		Resolver:                resolver,
		RegistryValidator:       registryValidator,
		UniqueRemoteURLsChecker: uniqueRemoteURLs,
	})
}
