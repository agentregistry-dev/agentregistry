package builtins

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

func init() {
	Register(Binding{
		Kind: v1alpha1.KindSkill,
		Wire: func(api huma.API, cfg resource.Config) {
			newObj := func() *v1alpha1.Skill { return &v1alpha1.Skill{} }
			resource.RegisterReadme(api, cfg, newObj, func(obj *v1alpha1.Skill) *v1alpha1.Readme {
				return obj.Spec.Readme
			})
			resource.Register(api, cfg, newObj)
		},
	})
}
