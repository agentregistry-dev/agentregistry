// Package v1alpha1crud wires the generic CRUD HTTP handlers for every
// first-party v1alpha1 Kind shipped by this repo (Agent, MCPServer,
// Skill, Prompt, Runtime, Deployment). Per-kind registration is a
// single `register(...)` call in bindings.go's init(); resource.Register
// handles every per-kind quirk internally (per-kind authz / list
// filtering / post-upsert / post-delete threaded through PerKindHooks).
//
// Scope: only the per-kind CRUD surface. Tagged artifacts use
// `/v0/{plural}/{name}/{tag}`; mutable objects use `/v0/{plural}/{name}`.
// Other v1alpha1 HTTP endpoints live in sibling packages, for example
// `/v0/deployments/{name}/logs` in deploymentlogs.
//
// First-party only: extension kinds (e.g. Role) do NOT
// register here — they wire their own resource.Register[T] call from
// AppOptions.ExtraRoutes (see pkg/types/types.go).
package crud

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// PerKindHooks groups optional, per-kind callbacks layered on top of
// the shared per-call config. Wired by downstream builds that need to
// inject authorization / filtering per resource kind without forking
// the OSS registration. Both maps are keyed by canonical Kind name
// (v1alpha1.KindAgent etc.); missing keys are treated as "no hook
// for this kind".
type PerKindHooks struct {
	// Authorizers gates every read + write operation per kind; see
	// resource.Config.Authorize for the contract.
	Authorizers map[string]func(ctx context.Context, in resource.AuthorizeInput) error
	// ListFilters injects ExtraWhere predicates into list queries per
	// kind; see resource.Config.ListFilter.
	ListFilters map[string]func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error)
	// PostUpserts run after a successful PUT; see resource.Config.PostUpsert.
	// Wired by downstream builds that need to mirror state into a
	// type-specific sidecar table on Runtime apply, drive a
	// reconciler, etc. Missing keys = no post-upsert hook for that kind.
	PostUpserts map[string]func(ctx context.Context, obj v1alpha1.Object) error
	// PostDeletes run after a successful DELETE; see
	// resource.Config.PostDelete. Mirrors PostUpserts above.
	PostDeletes map[string]func(ctx context.Context, obj v1alpha1.Object) error
	// Prepares run after validation and before Store.Upsert; see
	// resource.Config.Prepare. Wired by downstream builds that need to
	// mutate the decoded object before persistence (e.g. strip
	// sensitive spec fields). Missing keys = no prepare hook for that kind.
	Prepares map[string]func(ctx context.Context, obj v1alpha1.Object) error
	// InitialFinalizers seeds create-time finalizers per kind; see
	// resource.Config.InitialFinalizers.
	InitialFinalizers map[string]func(obj v1alpha1.Object) []string
}

// Register wires the namespace-scoped + cross-namespace list endpoints for
// registered v1alpha1 kinds against the supplied Stores map (as produced by
// v1alpha1store.NewStores). Each kind shares the same BasePrefix and cross-kind
// Resolver.
//
// Kinds with no Store entry or no registered typed binding are silently
// skipped; callers that want strict behavior should validate the maps ahead of
// the call. Go generics still require a concrete type at the route-registration
// call site, so bindings.go remains the small typed companion to the generic
// v1alpha1 kind registry.
func Register(
	api huma.API,
	basePrefix string,
	stores map[string]*v1alpha1store.Store,
	resolver v1alpha1.ResolverFunc,
	registryValidator v1alpha1.RegistryValidatorFunc,
	perKind PerKindHooks,
	deleteAdmission types.DeleteAdmission,
) {
	cfgFor := func(kind string) (resource.Config, bool) {
		store, ok := stores[kind]
		if !ok {
			return resource.Config{}, false
		}
		return resource.Config{
			Kind:               kind,
			BasePrefix:         basePrefix,
			Store:              store,
			Resolver:           resolver,
			RegistryValidator:  registryValidator,
			Authorize:          perKind.Authorizers[kind],
			ListFilter:         perKind.ListFilters[kind],
			EnableOriginFilter: kind == v1alpha1.KindDeployment,
			PostUpsert:         perKind.PostUpserts[kind],
			PostDelete:         perKind.PostDeletes[kind],
			Prepare:            perKind.Prepares[kind],
			DeleteAdmission:    deleteAdmission,
			InitialFinalizers:  perKind.InitialFinalizers[kind],
		}, true
	}

	for _, kind := range v1alpha1.RegisteredKinds() {
		cfg, ok := cfgFor(kind)
		if !ok {
			continue
		}
		wire, ok := bindings[kind]
		if !ok {
			continue
		}
		wire(api, cfg)
	}
}
