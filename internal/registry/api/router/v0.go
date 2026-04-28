// Package router contains API routing logic
package router

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deploymentlogs"
	v0embeddings "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/embeddings"
	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/importpipeline"
	v0ping "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/ping"
	v0version "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/version"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/danielgtaylor/huma/v2"
)

// Stores is the per-kind Store map used by the v1alpha1
// resource handler, keyed by v1alpha1 Kind name (e.g. "Agent",
// "MCPServer"). Produced by v1alpha1store.NewStores; enterprise
// builds may extend the map with additional kinds before passing it
// in.
type Stores = map[string]*v1alpha1store.Store

// RouteOptions contains the services that drive route registration.
//
// Stores is required; everything else is optional and gates a
// specific feature area (deployments, embeddings, semantic search).
// RegisterRoutes returns an error if a required field is missing rather
// than silently no-op'ing — a misconfigured boot fails loud.
type RouteOptions struct {
	// Stores is the per-kind v1alpha1store map that drives the
	// generic CRUD handlers at `/v0/{plural}/{name}/{version}?namespace={ns}`
	// (namespace is a query param defaulting to "default";
	// `?namespace=all` widens list scope across every namespace).
	// REQUIRED — RegisterRoutes errors when this is nil/empty.
	Stores Stores

	// Importer, when non-nil, enables POST /v0/import. Typically
	// constructed alongside Stores at bootstrap with the OSS
	// scanner set (OSV + Scorecard) + a FindingsStore bound to the
	// same pool.
	Importer *importer.Importer

	// DeploymentCoordinator drives post-persist reconciliation
	// for the Deployment kind: PUT → adapter.Apply; DELETE → adapter.Remove.
	// Constructed alongside Stores at bootstrap, wired into the
	// generic resource handler as a per-kind PostUpsert/PostDelete hook
	// for KindDeployment. Nil disables Deployment reconciliation — PUT
	// still persists the row, DELETE still soft-deletes, but no adapter
	// dispatch happens.
	DeploymentCoordinator *deploymentsvc.Coordinator

	// Indexer, when non-nil, enables POST /v0/embeddings/index +
	// GET /v0/embeddings/index/{jobId}. Constructed at bootstrap only
	// when AGENT_REGISTRY_EMBEDDINGS_ENABLED is set and a provider is
	// reachable; nil disables the endpoints. The in-process job tracker
	// is owned by the embeddings handler — there's no shared job
	// manager concept across the API.
	Indexer *embeddings.Indexer

	// SemanticSearch, when non-nil, enables
	// `?semantic=<q>&semanticThreshold=<f>` on list endpoints. The
	// func embeds the query string and returns the vector; the list
	// handler then routes through Store.SemanticList.
	SemanticSearch resource.SemanticSearchFunc

	// Authz gates admin-scope handlers (e.g. embeddings indexing) on
	// an API level. Nil falls back to the public provider so the
	// handlers register but every admin check short-circuits to allow.
	Authz auth.Authorizer

	// PerKindHooks injects per-kind Authorize + ListFilter
	// callbacks into the generic resource handler. Enterprise builds
	// thread their RBAC engine through here so reader / publisher /
	// admin gates fire on the OSS-registered Agent / MCPServer / Skill
	// / Prompt / Provider / Deployment endpoints. Zero-value matches
	// the public OSS default (no per-kind gates).
	PerKindHooks crud.PerKindHooks

	// RegistryValidator overrides the per-package registry
	// validator on the apply / import path. Nil falls back to
	// registries.Dispatcher, the upstream public-catalogue default.
	// See types.AppOptions.RegistryValidator for the full
	// rationale (private deployments typically swap in a filter that
	// short-circuits OCI).
	RegistryValidator v1alpha1.RegistryValidatorFunc

	// Optional callback for integration-owned route registration.
	ExtraRoutes func(api huma.API, pathPrefix string)
}

// RegisterRoutes registers all API routes under /v0. Required
// dependencies (RouteOptions itself, Stores) trigger an
// error rather than a silent skip so a misconfigured boot fails
// visibly.
func RegisterRoutes(
	api huma.API,
	cfg *config.Config,
	metrics *telemetry.Metrics,
	versionInfo *arv0.VersionBody,
	opts *RouteOptions,
) error {
	if opts == nil {
		return errors.New("router: RouteOptions is required")
	}
	if len(opts.Stores) == 0 {
		return errors.New("router: Stores is required")
	}

	pathPrefix := "/v0"

	v0health.RegisterHealthEndpoint(api, pathPrefix, cfg, metrics)
	v0ping.RegisterPingEndpoint(api, pathPrefix)
	v0version.RegisterVersionEndpoint(api, pathPrefix, versionInfo)

	// v1alpha1 generic routes. Cross-kind dangling-ref detection uses
	// a Store-backed resolver. Deployment reconciliation hooks plug in
	// when the coordinator is supplied.
	registerKindRoutes(
		api,
		pathPrefix,
		opts.Stores,
		opts.DeploymentCoordinator,
		opts.SemanticSearch,
		opts.PerKindHooks,
		opts.RegistryValidator,
	)

	// POST /v0/import — runs decoded manifests through the enrichment
	// pipeline (validate + scanners + findings-write) before Upsert.
	// Authorizers wires the same per-kind RBAC the regular apply path
	// uses; without it the import endpoint would be a write-bypass.
	if opts.Importer != nil {
		importpipeline.Register(api, importpipeline.Config{
			BasePrefix:  pathPrefix,
			Importer:    opts.Importer,
			Authorizers: opts.PerKindHooks.Authorizers,
		})
	}

	// Embeddings indexer endpoints — wired only when the indexer is
	// present. Authz gates admin-only operations; when zero-valued it
	// falls through to the public provider which allows every check,
	// matching the historical OSS default.
	if opts.Indexer != nil {
		v0embeddings.Register(api, v0embeddings.Config{
			BasePrefix: pathPrefix,
			Indexer:    opts.Indexer,
			Authz:      opts.Authz,
		})
	}

	if opts.ExtraRoutes != nil {
		opts.ExtraRoutes(api, pathPrefix)
	}
	return nil
}

// registerKindRoutes wires the generic resource handler for every
// built-in kind at `{basePrefix}/{plural}/{name}/{version}` (with
// namespace as a `?namespace={ns}` query param, default "default";
// `?namespace=all` on list widens scope across every namespace), plus
// the multi-doc apply endpoint at `{basePrefix}/apply`. Cross-kind ResourceRef
// existence dispatches through the shared
// internaldb.NewResolver so the router and any server-side
// Importer both see the same ref-existence semantics.
//
// When coord is non-nil, Deployment PUT/DELETE fire
// coord.Apply/coord.Remove after the row is persisted so the platform
// adapter converges runtime state synchronously with the API call.
func registerKindRoutes(api huma.API, basePrefix string, stores Stores, coord *deploymentsvc.Coordinator, semantic resource.SemanticSearchFunc, perKind crud.PerKindHooks, registryValidator v1alpha1.RegistryValidatorFunc) {
	resolver := internaldb.NewResolver(stores)
	if registryValidator == nil {
		registryValidator = registries.Dispatcher
	}
	// When a Deployment coordinator is supplied, install its Apply/Remove
	// as the KindDeployment PostUpsert/PostDelete. Deployment
	// reconciliation is a reserved seam in the v1alpha1 generic handler:
	// the coordinator hooks override any caller-supplied Deployment
	// hook so PUT/DELETE always drive the platform adapter. The same
	// hook table feeds both the per-kind PUT/DELETE handlers and the
	// /v0/apply batch path so a Deployment in a multi-doc apply
	// reconciles identically to a single-resource apply.
	if coord != nil {
		if perKind.PostUpserts == nil {
			perKind.PostUpserts = map[string]func(context.Context, v1alpha1.Object) error{}
		}
		if perKind.PostDeletes == nil {
			perKind.PostDeletes = map[string]func(context.Context, v1alpha1.Object) error{}
		}
		perKind.PostUpserts[v1alpha1.KindDeployment] = func(ctx context.Context, obj v1alpha1.Object) error {
			dep, ok := obj.(*v1alpha1.Deployment)
			if !ok {
				return nil
			}
			return coord.Apply(ctx, dep)
		}
		perKind.PostDeletes[v1alpha1.KindDeployment] = func(ctx context.Context, obj v1alpha1.Object) error {
			dep, ok := obj.(*v1alpha1.Deployment)
			if !ok {
				return nil
			}
			return coord.Remove(ctx, dep)
		}
	}

	// Per-kind CRUD endpoints — one call per built-in kind, hidden
	// inside crud.Register.
	crud.Register(api, basePrefix, stores, resolver, registryValidator, semantic, perKind)

	// Deployment-specific endpoints: logs stream (cancel is subsumed
	// by DesiredState=undeployed + DELETE in the v1alpha1 lifecycle).
	if coord != nil {
		deploymentlogs.Register(api, deploymentlogs.Config{
			BasePrefix:  basePrefix,
			Store:       stores[v1alpha1.KindDeployment],
			Coordinator: coord,
			Authorize:   perKind.Authorizers[v1alpha1.KindDeployment],
		})
	}

	// Multi-doc YAML batch apply at POST {basePrefix}/apply shares the
	// same per-kind hook table populated above, so Deployment reconciliation
	// and any caller-supplied PostUpsert/PostDelete fire identically on
	// the batch path.
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix:        basePrefix,
		Stores:            stores,
		Resolver:          resolver,
		RegistryValidator: registryValidator,
		Authorizers:       perKind.Authorizers,
		PostUpserts:       perKind.PostUpserts,
		PostDeletes:       perKind.PostDeletes,
	})
}
