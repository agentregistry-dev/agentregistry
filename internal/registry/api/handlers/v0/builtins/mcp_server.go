package builtins

import (
	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

func init() {
	Register(Binding{
		Kind: v1alpha1.KindMCPServer,
		Wire: func(api huma.API, cfg resource.Config) {
			newObj := func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} }
			resource.RegisterReadme(api, cfg, newObj, func(obj *v1alpha1.MCPServer) *v1alpha1.Readme {
				return obj.Spec.Readme
			})
			resource.Register(api, cfg, newObj)
			// Legacy `/v0/servers/{name}/readme` alias — predates the
			// generic readme subresource and is kept as a stable URL
			// for older MCP clients.
			resource.RegisterLegacyServerReadme(api, cfg)
		},
	})
}
