// Package router contains API routing logic
package router

import (
	"context"
	"errors"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deploymentlogs"
	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	v0ping "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/ping"
	v0version "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/version"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
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
// specific feature area (deployments).
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

	// DeploymentLogResolver supports the Deployment logs subresource. Adapter
	// Apply/Remove side effects are owned by the Deployment controller, not by
	// CRUD hook wiring.
	DeploymentLogResolver deploymentlogs.LogResolver

	// PerKindHooks injects per-kind Authorize + ListFilter
	// callbacks into the generic resource handler. Downstream integrations
	// thread their RBAC engine through here so reader / publisher /
	// admin gates fire on the OSS-registered Agent / MCPServer / Skill
	// / Prompt / Runtime / Deployment endpoints. Zero-value matches
	// the public OSS default (no per-kind gates).
	PerKindHooks crud.PerKindHooks

	// RegistryValidator overrides the per-package registry
	// validator on the apply path. Nil falls back to
	// registries.Dispatcher, the upstream public-catalogue default.
	// See types.AppOptions.RegistryValidator for the full
	// rationale (private deployments typically swap in a filter that
	// short-circuits OCI).
	RegistryValidator v1alpha1.RegistryValidatorFunc

	// Optional callback for integration-owned route registration.
	ExtraRoutes func(api huma.API, pathPrefix string)

	// Admission optionally owns the final apply write. Nil preserves OSS
	// production writes through resource.ProductionAdmission.
	// TODO(controller): temporary synchronous-handler bridge; remove when
	// reconciler-owned admission/staging exists.
	Admission types.Admission

	// DeleteAdmission optionally owns the final delete. Nil preserves OSS
	// production deletes through resource.ProductionDeleteAdmission.
	// TODO(controller): temporary synchronous-handler bridge; remove when
	// reconciler-owned admission/staging exists.
	DeleteAdmission types.DeleteAdmission

	// ResolverWrapper decorates the shared ResourceRef resolver before
	// resource and apply routes are registered.
	// TODO(controller): temporary bridge for pending staged refs during HTTP apply.
	ResolverWrapper func(v1alpha1.ResolverFunc) v1alpha1.ResolverFunc

	// ExtraResourceRoutes registers adjacent routes with access to the same
	// v1alpha1 stores and hooks used by /v0/apply.
	// TODO(controller): temporary bridge for downstream synchronous approval routes.
	ExtraResourceRoutes func(api huma.API, pathPrefix string, ctx types.ResourceRouteContext)
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
	// a Store-backed resolver. Deployment side effects are handled by
	// the always-on Deployment controller after the row is persisted.
	registerKindRoutes(
		api,
		pathPrefix,
		opts.Stores,
		opts.DeploymentLogResolver,
		opts.PerKindHooks,
		opts.RegistryValidator,
		opts.Admission,
		opts.DeleteAdmission,
		opts.ResolverWrapper,
		opts.ExtraResourceRoutes,
	)

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
// dispatches through the shared internaldb.NewResolver.
func registerKindRoutes(
	api huma.API,
	basePrefix string,
	stores Stores,
	logResolver deploymentlogs.LogResolver,
	perKind crud.PerKindHooks,
	registryValidator v1alpha1.RegistryValidatorFunc,
	admission types.Admission,
	deleteAdmission types.DeleteAdmission,
	resolverWrapper func(v1alpha1.ResolverFunc) v1alpha1.ResolverFunc,
	extraResourceRoutes func(api huma.API, pathPrefix string, ctx types.ResourceRouteContext),
) resource.ApplyConfig {
	resolver := internaldb.NewResolver(stores)
	if resolverWrapper != nil {
		resolver = resolverWrapper(resolver)
	}
	if registryValidator == nil {
		registryValidator = registries.Dispatcher
	}
	// Per-kind CRUD endpoints — one call per built-in kind, hidden
	// inside crud.Register.
	crud.Register(api, basePrefix, stores, resolver, registryValidator, perKind, deleteAdmission)

	// Deployment-specific endpoints: logs stream (cancel is subsumed
	// by DesiredState=undeployed + DELETE in the v1alpha1 lifecycle).
	if logResolver != nil {
		deploymentlogs.Register(api, deploymentlogs.Config{
			BasePrefix:  basePrefix,
			Store:       stores[v1alpha1.KindDeployment],
			LogResolver: logResolver,
			Authorize:   perKind.Authorizers[v1alpha1.KindDeployment],
		})
	}

	// Multi-doc YAML batch apply at POST {basePrefix}/apply shares the
	// same per-kind hook table populated above, so Deployment reconciliation
	// and any caller-supplied PostUpsert/PostDelete fire identically on
	// the batch path.
	// ApplyConfig.Prepare is a single global hook, not a per-kind map, so
	// dispatch by Kind over the per-kind Prepares table to match the
	// dedicated PUT route's per-kind wiring.
	var applyPrepare func(ctx context.Context, obj v1alpha1.Object) error
	if len(perKind.Prepares) > 0 {
		prepares := perKind.Prepares
		applyPrepare = func(ctx context.Context, obj v1alpha1.Object) error {
			if p := prepares[obj.GetKind()]; p != nil {
				return p(ctx, obj)
			}
			return nil
		}
	}
	applyCfg := resource.ApplyConfig{
		BasePrefix:        basePrefix,
		Stores:            stores,
		Resolver:          resolver,
		RegistryValidator: registryValidator,
		Authorizers:       perKind.Authorizers,
		PostUpserts:       perKind.PostUpserts,
		PostDeletes:       perKind.PostDeletes,
		InitialFinalizers: perKind.InitialFinalizers,
		Admission:         admission,
		DeleteAdmission:   deleteAdmission,
		Prepare:           applyPrepare,
	}
	productionApplyCfg := applyCfg
	productionApplyCfg.Admission = resource.ProductionAdmission
	productionDeleteCfg := applyCfg
	productionDeleteCfg.DeleteAdmission = resource.ProductionDeleteAdmission
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
			Delete: func(ctx context.Context, obj v1alpha1.Object, dryRun bool) arv0.ApplyResult {
				return resource.DeleteObject(ctx, productionDeleteCfg, obj, dryRun)
			},
		})
	}
	return applyCfg
}
