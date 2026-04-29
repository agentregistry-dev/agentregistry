// Package v1alpha1crud wires the generic CRUD HTTP handlers for every
// first-party v1alpha1 Kind shipped by this repo (Agent, MCPServer,
// Skill, Prompt, Provider, Deployment). Per-kind registration is a
// single `register(...)` call in bindings.go's init(); resource.Register
// handles every per-kind quirk internally (readme subresource
// auto-detected via v1alpha1.ObjectWithReadme; per-kind authz / list
// filtering / post-upsert / post-delete threaded through PerKindHooks).
//
// Scope: only the per-kind CRUD surface (`/v0/{plural}/{name}/{version}`
// + the readme subresource for kinds that have one). Other v1alpha1
// HTTP endpoints live in sibling packages — `/v0/import` in
// importpipeline, `/v0/deployments/{name}/{version}/logs` in
// deploymentlogs.
//
// First-party only: enterprise extension kinds (e.g. Role) do NOT
// register here — they wire their own resource.Register[T] call from
// AppOptions.ExtraRoutes (see pkg/types/types.go).
package crud

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// PerKindHooks groups optional, per-kind callbacks layered on top of
// the shared per-call config. Wired by enterprise builds that need to
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
	// Wired by enterprise builds that need to mirror state into a
	// platform-specific sidecar table on Provider apply, drive a
	// reconciler, etc. Missing keys = no post-upsert hook for that kind.
	PostUpserts map[string]func(ctx context.Context, obj v1alpha1.Object) error
	// PostDeletes run after a successful DELETE; see
	// resource.Config.PostDelete. Mirrors PostUpserts above.
	PostDeletes map[string]func(ctx context.Context, obj v1alpha1.Object) error
}

// Register wires the namespace-scoped + cross-namespace list endpoints
// for every built-in v1alpha1 Kind against the supplied Stores map (as
// produced by v1alpha1store.NewStores). Each kind shares the
// same BasePrefix and cross-kind Resolver.
//
// Iteration order is fixed by v1alpha1.BuiltinKinds so OpenAPI output
// stays stable across builds. Kinds in BuiltinKinds with no Store
// entry or no registered binding are silently skipped; callers that
// want strict behavior should validate the maps ahead of the call.
//
// Adding a new kind: append to v1alpha1.BuiltinKinds and add one
// `register(...)` call in bindings.go's init().
func Register(
	api huma.API,
	basePrefix string,
	stores map[string]*v1alpha1store.Store,
	resolver v1alpha1.ResolverFunc,
	registryValidator v1alpha1.RegistryValidatorFunc,
	semanticSearch resource.SemanticSearchFunc,
	perKind PerKindHooks,
) {
	cfgFor := func(kind string) (resource.Config, bool) {
		store, ok := stores[kind]
		if !ok {
			return resource.Config{}, false
		}
		return resource.Config{
			Kind:              kind,
			BasePrefix:        basePrefix,
			Store:             store,
			Resolver:          resolver,
			RegistryValidator: registryValidator,
			SemanticSearch:    semanticSearch,
			Authorize:         perKind.Authorizers[kind],
			ListFilter:        perKind.ListFilters[kind],
			PostUpsert:        perKind.PostUpserts[kind],
			PostDelete:        perKind.PostDeletes[kind],
		}, true
	}

	for _, kind := range v1alpha1.BuiltinKinds {
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
