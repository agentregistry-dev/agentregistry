package resource

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

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
			Register[*v1alpha1.Deployment](api, cfg, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
		}
	}
}
