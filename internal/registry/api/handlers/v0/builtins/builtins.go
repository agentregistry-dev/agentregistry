package builtins

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
// the OSS builtins registration. Both maps are keyed by canonical
// Kind name (v1alpha1.KindAgent etc.); missing keys are treated as
// "no hook for this kind".
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

// RegisterBuiltins wires the namespace-scoped + cross-namespace
// endpoints for every built-in v1alpha1 Kind against the supplied
// Stores map (as produced by database.NewV1Alpha1Stores). Each kind
// shares the same BasePrefix and cross-kind Resolver.
//
// Kinds are registered in v1alpha1.BuiltinKinds order so OpenAPI
// output stays stable across builds. Kinds present in BuiltinKinds
// but missing from stores are silently skipped — callers that want
// strict behavior should validate the map ahead of the call.
//
// Enterprise / downstream builds with additional Kinds should call
// resource.Register[T] directly for each custom kind; RegisterBuiltins owns
// only the OSS set.
//
// Because Go generics resolve at compile time, the six type
// parameters must be named explicitly here. Adding a new built-in
// kind means updating v1alpha1.BuiltinKinds, V1Alpha1TableFor in the
// database package, AND the switch below.
func RegisterBuiltins(
	api huma.API,
	basePrefix string,
	stores map[string]*v1alpha1store.Store,
	resolver v1alpha1.ResolverFunc,
	registryValidator v1alpha1.RegistryValidatorFunc,
	uniqueRemoteURLsChecker v1alpha1.UniqueRemoteURLsFunc,
	semanticSearch resource.SemanticSearchFunc,
	perKind PerKindHooks,
) {
	cfgFor := func(kind string) (resource.Config, bool) {
		store, ok := stores[kind]
		if !ok {
			return resource.Config{}, false
		}
		return resource.Config{
			Kind:                    kind,
			BasePrefix:              basePrefix,
			Store:                   store,
			Resolver:                resolver,
			RegistryValidator:       registryValidator,
			UniqueRemoteURLsChecker: uniqueRemoteURLsChecker,
			SemanticSearch:          semanticSearch,
			Authorize:               perKind.Authorizers[kind],
			ListFilter:              perKind.ListFilters[kind],
			PostUpsert:              perKind.PostUpserts[kind],
			PostDelete:              perKind.PostDeletes[kind],
		}, true
	}

	for _, kind := range v1alpha1.BuiltinKinds {
		cfg, ok := cfgFor(kind)
		if !ok {
			continue
		}
		switch kind {
		case v1alpha1.KindAgent:
			newObj := func() *v1alpha1.Agent { return &v1alpha1.Agent{} }
			// resource.RegisterReadme before resource.Register so the literal
			// `/{name}/readme` path wins over the generic
			// `/{name}/{version}` catch-all when their depths collide.
			resource.RegisterReadme[*v1alpha1.Agent](api, cfg, newObj, func(obj *v1alpha1.Agent) *v1alpha1.Readme {
				return obj.Spec.Readme
			})
			resource.Register[*v1alpha1.Agent](api, cfg, newObj)
		case v1alpha1.KindMCPServer:
			newObj := func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} }
			resource.RegisterReadme[*v1alpha1.MCPServer](api, cfg, newObj, func(obj *v1alpha1.MCPServer) *v1alpha1.Readme {
				return obj.Spec.Readme
			})
			resource.Register[*v1alpha1.MCPServer](api, cfg, newObj)
			resource.RegisterLegacyServerReadme(api, basePrefix, cfg.Store)
		case v1alpha1.KindSkill:
			newObj := func() *v1alpha1.Skill { return &v1alpha1.Skill{} }
			resource.RegisterReadme[*v1alpha1.Skill](api, cfg, newObj, func(obj *v1alpha1.Skill) *v1alpha1.Readme {
				return obj.Spec.Readme
			})
			resource.Register[*v1alpha1.Skill](api, cfg, newObj)
		case v1alpha1.KindPrompt:
			newObj := func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} }
			resource.RegisterReadme[*v1alpha1.Prompt](api, cfg, newObj, func(obj *v1alpha1.Prompt) *v1alpha1.Readme {
				return obj.Spec.Readme
			})
			resource.Register[*v1alpha1.Prompt](api, cfg, newObj)
		case v1alpha1.KindProvider:
			resource.Register[*v1alpha1.Provider](api, cfg, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
		case v1alpha1.KindDeployment:
			resource.Register[*v1alpha1.Deployment](api, cfg, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
		}
	}
}
