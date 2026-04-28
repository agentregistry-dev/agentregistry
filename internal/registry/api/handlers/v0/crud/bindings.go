package crud

import (
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

// bindings maps a Kind name to the typed Wire closure that knows how
// to call resource.Register[T] with the concrete envelope. Populated
// by init() below — adding a new built-in kind is one new register()
// call. The Wire indirection is the smallest layer compatible with Go
// generics: resource.Register[T] needs a concrete T at the call site,
// and the typed closure is exactly that.
//
// The generic resource.Register handles every per-kind quirk
// internally — readme subresource auto-registers when T implements
// v1alpha1.ObjectWithReadme; per-kind authz / list filtering /
// post-upsert / post-delete flow through resource.Config. There are
// no remaining bespoke per-kind code paths in this package.
var bindings = map[string]func(api huma.API, cfg resource.Config){}

func register[T v1alpha1.Object](kind string, newObj func() T) {
	if _, dup := bindings[kind]; dup {
		panic(fmt.Sprintf("builtins: kind %q already registered", kind))
	}
	bindings[kind] = func(api huma.API, cfg resource.Config) {
		resource.Register(api, cfg, newObj)
	}
}

func init() {
	register(v1alpha1.KindAgent, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
	register(v1alpha1.KindMCPServer, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
	register(v1alpha1.KindSkill, func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
	register(v1alpha1.KindPrompt, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
	register(v1alpha1.KindProvider, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
	register(v1alpha1.KindDeployment, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
}
