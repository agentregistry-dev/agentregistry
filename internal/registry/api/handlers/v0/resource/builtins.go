package resource

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// DeploymentHooks groups post-persist callbacks used by RegisterBuiltins
// to drive adapter reconciliation on the Deployment kind. Callers supply
// one instance per registry build — typically wrapping a
// V1Alpha1Coordinator. Nil hooks are ignored.
type DeploymentHooks struct {
	// PostUpsert runs after a Deployment PUT persists the row. Intended
	// to call coordinator.Apply which resolves refs + dispatches to the
	// platform adapter + patches status.
	PostUpsert func(ctx context.Context, deployment *v1alpha1.Deployment) error
	// PostDelete runs after a Deployment DELETE sets DeletionTimestamp.
	// Intended to call coordinator.Remove which tears down runtime
	// resources + drops the adapter finalizer.
	PostDelete func(ctx context.Context, deployment *v1alpha1.Deployment) error
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
// Register[T] directly for each custom kind; RegisterBuiltins owns
// only the OSS set.
//
// Because Go generics resolve at compile time, the six type
// parameters must be named explicitly here. Adding a new built-in
// kind means updating v1alpha1.BuiltinKinds, V1Alpha1TableFor in the
// database package, AND the switch below.
func RegisterBuiltins(
	api huma.API,
	basePrefix string,
	stores map[string]*database.Store,
	resolver v1alpha1.ResolverFunc,
	registryValidator v1alpha1.RegistryValidatorFunc,
	uniqueRemoteURLsChecker v1alpha1.UniqueRemoteURLsFunc,
	deploymentHooks DeploymentHooks,
) {
	cfgFor := func(kind string) (Config, bool) {
		store, ok := stores[kind]
		if !ok {
			return Config{}, false
		}
		return Config{
			Kind:                    kind,
			BasePrefix:              basePrefix,
			Store:                   store,
			Resolver:                resolver,
			RegistryValidator:       registryValidator,
			UniqueRemoteURLsChecker: uniqueRemoteURLsChecker,
		}, true
	}

	for _, kind := range v1alpha1.BuiltinKinds {
		cfg, ok := cfgFor(kind)
		if !ok {
			continue
		}
		switch kind {
		case v1alpha1.KindAgent:
			Register[*v1alpha1.Agent](api, cfg, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		case v1alpha1.KindMCPServer:
			Register[*v1alpha1.MCPServer](api, cfg, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		case v1alpha1.KindSkill:
			Register[*v1alpha1.Skill](api, cfg, func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		case v1alpha1.KindPrompt:
			Register[*v1alpha1.Prompt](api, cfg, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
		case v1alpha1.KindProvider:
			Register[*v1alpha1.Provider](api, cfg, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
		case v1alpha1.KindDeployment:
			if deploymentHooks.PostUpsert != nil {
				cfg.PostUpsert = func(ctx context.Context, obj v1alpha1.Object) error {
					return deploymentHooks.PostUpsert(ctx, obj.(*v1alpha1.Deployment))
				}
			}
			if deploymentHooks.PostDelete != nil {
				cfg.PostDelete = func(ctx context.Context, obj v1alpha1.Object) error {
					return deploymentHooks.PostDelete(ctx, obj.(*v1alpha1.Deployment))
				}
			}
			Register[*v1alpha1.Deployment](api, cfg, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
		}
	}
}
