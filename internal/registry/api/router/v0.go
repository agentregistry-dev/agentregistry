// Package router contains API routing logic
package router

import (
	"net/http"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"

	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	v0embeddings "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/embeddings"
	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	v0ping "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/ping"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/resource"
	v0version "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/version"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
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

// RegistryServices bundles the per-domain service registries that the
// remaining legacy handlers + deployment orchestration still require.
// Every field here is a port target for the v1alpha1 cutover:
//   - Server / Agent / Skill feed MCP registryserver tools (Group 9)
//     and platform/utils deployment target lookups (Group 5).
//   - Provider feeds the deployment service's platform selection.
//   - Deployment still owns the SSE watch + logs + cancel RPCs (B1.f).
type RegistryServices struct {
	Server     serversvc.Registry
	Agent      agentsvc.Registry
	Skill      skillsvc.Registry
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

	// Optional provider adapters keyed by platform. Deployment adapters
	// live on the coordinator below.
	ProviderPlatforms map[string]registrytypes.ProviderPlatformAdapter

	// V1Alpha1Stores, when non-empty, enables the generic v1alpha1 handler
	// at `/v0/namespaces/{ns}/{plural}/...`. Legacy routes stay live
	// regardless; clients migrate off them incrementally.
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
	if opts != nil && opts.Indexer != nil && opts.JobManager != nil {
		v0embeddings.RegisterEmbeddingsEndpoints(api, pathPrefix, opts.Indexer, opts.JobManager)
		if opts.Mux != nil {
			v0embeddings.RegisterEmbeddingsSSEHandler(opts.Mux, pathPrefix, opts.Indexer, opts.JobManager)
		}
	}

	// v1alpha1 generic routes — wired when V1Alpha1Stores is provided.
	// Lives alongside the legacy per-kind routes; clients migrate over
	// time. Cross-kind dangling-ref detection uses a Store-backed
	// resolver. Deployment reconciliation hooks plug in when the
	// coordinator is supplied.
	if opts != nil && len(opts.V1Alpha1Stores) > 0 {
		registerV1Alpha1Routes(api, pathPrefix, opts.V1Alpha1Stores, opts.V1Alpha1DeploymentCoordinator)
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
//
// When coord is non-nil, Deployment PUT/DELETE fire
// coord.Apply/coord.Remove after the row is persisted so the platform
// adapter converges runtime state synchronously with the API call.
func registerV1Alpha1Routes(api huma.API, basePrefix string, stores V1Alpha1Stores, coord *deploymentsvc.V1Alpha1Coordinator) {
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
	resource.RegisterBuiltins(api, basePrefix, stores, resolver, registryValidator, uniqueRemoteURLs, hooks)

	// Deployment-specific endpoints: logs stream (cancel is subsumed
	// by DesiredState=undeployed + DELETE in the v1alpha1 lifecycle).
	if coord != nil {
		resource.RegisterDeploymentLogs(api, resource.DeploymentLogsConfig{
			BasePrefix:  basePrefix,
			Store:       stores[v1alpha1.KindDeployment],
			Coordinator: coord,
		})
	}

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
