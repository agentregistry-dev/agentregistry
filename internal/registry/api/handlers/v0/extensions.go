package v0

import registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"

// PlatformExtensions holds optional deployment adapter registries.
// Provider CRUD is now fully service/DB-backed.
type PlatformExtensions struct {
	ProviderPlatforms   map[string]registrytypes.ProviderPlatformAdapter
	DeploymentPlatforms map[string]registrytypes.DeploymentPlatformAdapter
}

func (e PlatformExtensions) ResolveProviderAdapter(platform string) (registrytypes.ProviderPlatformAdapter, bool) {
	if e.ProviderPlatforms == nil {
		return nil, false
	}
	adapter, ok := e.ProviderPlatforms[platform]
	return adapter, ok
}

func (e PlatformExtensions) ResolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, bool) {
	if e.DeploymentPlatforms == nil {
		return nil, false
	}
	adapter, ok := e.DeploymentPlatforms[platform]
	return adapter, ok
}
