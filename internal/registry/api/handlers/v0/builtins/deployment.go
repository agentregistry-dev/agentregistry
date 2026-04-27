package builtins

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

func init() {
	Register(Binding{
		Kind: v1alpha1.KindDeployment,
		Wire: func(api huma.API, cfg resource.Config) {
			resource.Register(api, cfg, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
		},
	})
}
