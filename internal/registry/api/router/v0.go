// Package router contains API routing logic
package router

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deploymentlogs"
	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/importpipeline"
	v0ping "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/ping"
	v0version "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/version"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/agentregistry-dev/agentregistry/pkg/importer"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
)

// Stores is the per-kind Store map used by the v1alpha1
// resource handler, keyed by v1alpha1 Kind name (e.g. "Agent",
// "MCPServer"). Produced by v1alpha1store.NewStores; downstream
// builds may extend the map with additional kinds before passing it
// in.
type Stores = map[string]*v1alpha1store.Store

// RouteOptions contains the services that drive route registration.
//
// Stores is required; everything else is optional and gates a
// specific feature area (deployments, import).
// RegisterRoutes returns an error if a required field is missing rather
// than silently no-op'ing — a misconfigured boot fails loud.
type RouteOptions struct {
	// Stores is the per-kind v1alpha1store map that drives the
	// generic CRUD handlers. Tagged artifacts expose
	// `/v0/{plural}/{name}/{tag}?namespace={ns}`; mutable objects expose
	// `/v0/{plural}/{name}?namespace={ns}`. Namespace defaults to
	// "default"; `?namespace=all` widens list scope across every
	// namespace.
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

	// PerKindHooks injects per-kind Authorize + ListFilter
	// callbacks into the generic resource handler. Downstream integrations
	// thread their RBAC engine through here so reader / publisher /
	// admin gates fire on the OSS-registered Agent / MCPServer / Skill
	// / Prompt / Runtime / Deployment endpoints. Zero-value matches
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

	// ApplyInterceptor optionally accepts a validated apply before
	// production Upsert. Nil preserves normal direct writes.
	ApplyInterceptor resource.ApplyInterceptor

	// ResolverWrapper decorates the shared ResourceRef resolver before
	// resource and apply routes are registered.
	ResolverWrapper func(v1alpha1.ResolverFunc) v1alpha1.ResolverFunc

	// ExtraResourceRoutes registers adjacent routes with access to the same
	// v1alpha1 stores and hooks used by /v0/apply.
	ExtraResourceRoutes func(api huma.API, pathPrefix string, ctx types.ResourceRouteContext)

	// ImportAuthorizers overrides PerKindHooks.Authorizers for /v0/import.
	// Nil preserves the regular authorizer map.
	ImportAuthorizers map[string]func(ctx context.Context, in resource.AuthorizeInput) error
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
		opts.PerKindHooks,
		opts.RegistryValidator,
		opts.ApplyInterceptor,
		opts.ResolverWrapper,
		opts.ExtraResourceRoutes,
	)

	// POST /v0/import — runs decoded manifests through the enrichment
	// pipeline (validate + scanners + findings-write) before Upsert.
	// Authorizers wires the same per-kind RBAC the regular apply path
	// uses; without it the import endpoint would be a write-bypass.
	if opts.Importer != nil {
		importAuthorizers := opts.PerKindHooks.Authorizers
		if opts.ImportAuthorizers != nil {
			importAuthorizers = opts.ImportAuthorizers
		}
		importpipeline.Register(api, importpipeline.Config{
			BasePrefix:  pathPrefix,
			Importer:    opts.Importer,
			Authorizers: importAuthorizers,
		})
	}

	if opts.ExtraRoutes != nil {
		opts.ExtraRoutes(api, pathPrefix)
	}
	return nil
}

// registerKindRoutes wires the generic resource handler for every
// built-in kind. Tagged artifacts use
// `{basePrefix}/{plural}/{name}/{tag}`; mutable objects use
// `{basePrefix}/{plural}/{name}`. Namespace is a `?namespace={ns}`
// query param defaulting to "default"; `?namespace=all` on list
// widens scope across every namespace. The multi-doc apply endpoint
// lives at `{basePrefix}/apply`. Cross-kind ResourceRef existence
// dispatches through the shared
// internaldb.NewResolver so the router and any server-side
// Importer both see the same ref-existence semantics.
//
// When coord is non-nil, Deployment PUT/DELETE fire
// coord.Apply/coord.Remove after the row is persisted so the type
// adapter converges runtime state synchronously with the API call.
func registerKindRoutes(
	api huma.API,
	basePrefix string,
	stores Stores,
	coord *deploymentsvc.Coordinator,
	perKind crud.PerKindHooks,
	registryValidator v1alpha1.RegistryValidatorFunc,
	applyInterceptor resource.ApplyInterceptor,
	resolverWrapper func(v1alpha1.ResolverFunc) v1alpha1.ResolverFunc,
	extraResourceRoutes func(api huma.API, pathPrefix string, ctx types.ResourceRouteContext),
) {
	resolver := internaldb.NewResolver(stores)
	if resolverWrapper != nil {
		resolver = resolverWrapper(resolver)
	}
	if registryValidator == nil {
		registryValidator = registries.Dispatcher
	}
	// When a Deployment coordinator is supplied, install its Apply/Remove
	// as the KindDeployment PostUpsert/PostDelete. Deployment
	// reconciliation is a reserved seam in the v1alpha1 generic handler:
	// the coordinator hooks override any caller-supplied Deployment
	// hook so PUT/DELETE always drive the type adapter. The same
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
	crud.Register(api, basePrefix, stores, resolver, registryValidator, perKind)

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
	applyCfg := resource.ApplyConfig{
		BasePrefix:        basePrefix,
		Stores:            stores,
		Resolver:          resolver,
		RegistryValidator: registryValidator,
		Authorizers:       perKind.Authorizers,
		PostUpserts:       perKind.PostUpserts,
		PostDeletes:       perKind.PostDeletes,
		InitialFinalizers: perKind.InitialFinalizers,
		ApplyInterceptor:  applyInterceptor,
	}
	productionApplyCfg := applyCfg
	productionApplyCfg.ApplyInterceptor = nil
	resource.RegisterApply(api, applyCfg)

	if extraResourceRoutes != nil {
		opaqueStores := make(map[string]any, len(stores))
		for kind, store := range stores {
			opaqueStores[kind] = store
		}
		extraResourceRoutes(api, basePrefix, types.ResourceRouteContext{
			Stores:            opaqueStores,
			Resolver:          resolver,
			RegistryValidator: registryValidator,
			Apply: func(ctx context.Context, obj v1alpha1.Object, dryRun bool) arv0.ApplyResult {
				return resource.ApplyObject(ctx, productionApplyCfg, obj, dryRun)
			},
		})
	}
}
