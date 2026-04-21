package extensions

import registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"

// PlatformExtensions holds optional platform adapter registries. The
// legacy deployment-adapter map was deleted alongside the legacy
// deployment service; only provider-side adapters remain.
type PlatformExtensions struct {
	ProviderPlatforms map[string]registrytypes.ProviderPlatformAdapter
}

func (e PlatformExtensions) ResolveProviderAdapter(platform string) (registrytypes.ProviderPlatformAdapter, bool) {
	if e.ProviderPlatforms == nil {
		return nil, false
	}
	adapter, ok := e.ProviderPlatforms[platform]
	return adapter, ok
}
