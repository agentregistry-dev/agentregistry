package builtins

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

func init() {
	Register(Binding{
		Kind: v1alpha1.KindAgent,
		Wire: func(api huma.API, cfg resource.Config) {
			newObj := func() *v1alpha1.Agent { return &v1alpha1.Agent{} }
			// RegisterReadme runs before Register so the literal
			// `/{name}/readme` path beats the generic
			// `/{name}/{version}` catch-all at the shared depth.
			resource.RegisterReadme(api, cfg, newObj, func(obj *v1alpha1.Agent) *v1alpha1.Readme {
				return obj.Spec.Readme
			})
			resource.Register(api, cfg, newObj)
		},
	})
}
