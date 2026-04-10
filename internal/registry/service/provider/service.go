package provider

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

type Dependencies struct {
	StoreDB           database.Store
	Providers         database.ProviderStore
	ProviderPlatforms map[string]registrytypes.ProviderPlatformAdapter
}

// Does not embed database.ProviderReader because ListProviders normalizes the
// platform input to a plain string and applies adapter dispatch logic.
type Registry interface {
	ListProviders(ctx context.Context, platform string) ([]*models.Provider, error)
	RegisterProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error)
	GetProvider(ctx context.Context, providerID string) (*models.Provider, error)
	ResolveProvider(ctx context.Context, providerID, platformHint string) (*models.Provider, error)
	UpdateProvider(ctx context.Context, providerID, platformHint string, in *models.UpdateProviderInput) (*models.Provider, error)
	DeleteProvider(ctx context.Context, providerID, platformHint string) error
	PlatformAdapters() map[string]registrytypes.ProviderPlatformAdapter
}

type UnsupportedPlatformError struct {
	Platform string
}

func (e *UnsupportedPlatformError) Error() string {
	platform := normalizePlatform(e.Platform)
	if platform == "" {
		platform = "unknown"
	}
	return "provider platform is not supported: " + platform
}

func IsUnsupportedPlatformError(err error) bool {
	var unsupported *UnsupportedPlatformError
	return errors.As(err, &unsupported)
}

type registry struct {
	providers database.ProviderStore
	adapters  map[string]registrytypes.ProviderPlatformAdapter
}

var _ Registry = (*registry)(nil)

func New(deps Dependencies) Registry {
	if deps.Providers == nil && deps.StoreDB != nil {
		deps.Providers = deps.StoreDB.Providers()
	}

	// When StoreDB is provided (production path), build default platform adapters
	// and merge any caller-supplied overrides on top. When only Providers is set
	// directly (tests/custom wiring), use only what the caller supplies.
	var adapters map[string]registrytypes.ProviderPlatformAdapter
	if deps.StoreDB != nil && deps.Providers != nil {
		adapters = defaultPlatformAdapters(deps.Providers)
		maps.Copy(adapters, deps.ProviderPlatforms)
	} else if deps.ProviderPlatforms != nil {
		adapters = deps.ProviderPlatforms
	} else {
		adapters = map[string]registrytypes.ProviderPlatformAdapter{}
	}

	return &registry{
		providers: deps.Providers,
		adapters:  adapters,
	}
}

func (r *registry) PlatformAdapters() map[string]registrytypes.ProviderPlatformAdapter {
	return r.adapters
}

func (r *registry) ListProviders(ctx context.Context, platform string) ([]*models.Provider, error) {
	normalizedPlatform := normalizePlatform(platform)
	if normalizedPlatform != "" {
		if adapter, ok := r.resolveAdapter(normalizedPlatform); ok {
			return adapter.ListProviders(ctx)
		}
		if len(r.adapters) > 0 {
			return nil, &UnsupportedPlatformError{Platform: normalizedPlatform}
		}
		return r.providers.ListProviders(ctx, &normalizedPlatform)
	}

	if len(r.adapters) == 0 {
		return r.providers.ListProviders(ctx, nil)
	}

	providers := make([]*models.Provider, 0)
	seen := make(map[string]struct{})
	for _, adapterPlatform := range adapterPlatforms(r.adapters) {
		adapter := r.adapters[adapterPlatform]
		listed, err := adapter.ListProviders(ctx)
		if err != nil {
			return nil, err
		}
		for _, provider := range listed {
			if provider == nil {
				continue
			}
			if _, ok := seen[provider.ID]; ok {
				continue
			}
			seen[provider.ID] = struct{}{}
			providers = append(providers, provider)
		}
	}

	return providers, nil
}

func (r *registry) RegisterProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	if in == nil {
		return nil, database.ErrInvalidInput
	}

	platform := normalizePlatform(in.Platform)
	if platform == "" {
		return nil, fmt.Errorf("%w: provider platform is required", database.ErrInvalidInput)
	}
	in.Platform = platform

	if adapter, ok := r.resolveAdapter(platform); ok {
		return adapter.CreateProvider(ctx, in)
	}
	if len(r.adapters) > 0 {
		return nil, &UnsupportedPlatformError{Platform: platform}
	}
	return r.providers.CreateProvider(ctx, in)
}

func (r *registry) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	return r.ResolveProvider(ctx, providerID, "")
}

func (r *registry) ResolveProvider(ctx context.Context, providerID, platformHint string) (*models.Provider, error) {
	resolvedProviderID := strings.TrimSpace(providerID)
	if resolvedProviderID == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}

	platform := normalizePlatform(platformHint)
	if len(r.adapters) == 0 {
		return r.resolveProviderFromStore(ctx, resolvedProviderID, platform)
	}

	if platform != "" {
		adapter, ok := r.resolveAdapter(platform)
		if !ok {
			return nil, &UnsupportedPlatformError{Platform: platform}
		}
		return adapter.GetProvider(ctx, resolvedProviderID)
	}

	for _, adapterPlatform := range adapterPlatforms(r.adapters) {
		provider, err := r.adapters[adapterPlatform].GetProvider(ctx, resolvedProviderID)
		if err == nil && provider != nil {
			return provider, nil
		}
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return nil, err
		}
	}

	provider, err := r.providers.GetProvider(ctx, resolvedProviderID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, database.ErrNotFound
	}
	return nil, &UnsupportedPlatformError{Platform: provider.Platform}
}

func (r *registry) UpdateProvider(ctx context.Context, providerID, platformHint string, in *models.UpdateProviderInput) (*models.Provider, error) {
	provider, err := r.ResolveProvider(ctx, providerID, platformHint)
	if err != nil {
		return nil, err
	}

	platform := normalizePlatform(provider.Platform)
	if adapter, ok := r.resolveAdapter(platform); ok {
		return adapter.UpdateProvider(ctx, provider.ID, in)
	}
	if len(r.adapters) > 0 {
		return nil, &UnsupportedPlatformError{Platform: platform}
	}
	return r.providers.UpdateProvider(ctx, provider.ID, in)
}

func (r *registry) DeleteProvider(ctx context.Context, providerID, platformHint string) error {
	provider, err := r.ResolveProvider(ctx, providerID, platformHint)
	if err != nil {
		return err
	}

	platform := normalizePlatform(provider.Platform)
	if adapter, ok := r.resolveAdapter(platform); ok {
		return adapter.DeleteProvider(ctx, provider.ID)
	}
	if len(r.adapters) > 0 {
		return &UnsupportedPlatformError{Platform: platform}
	}
	return r.providers.DeleteProvider(ctx, provider.ID)
}

func (r *registry) resolveAdapter(platform string) (registrytypes.ProviderPlatformAdapter, bool) {
	adapter, ok := r.adapters[normalizePlatform(platform)]
	return adapter, ok
}

func (r *registry) resolveProviderFromStore(ctx context.Context, providerID, platform string) (*models.Provider, error) {
	provider, err := r.providers.GetProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil {
		return nil, database.ErrNotFound
	}
	if platform != "" && normalizePlatform(provider.Platform) != platform {
		return nil, database.ErrNotFound
	}
	return provider, nil
}

func adapterPlatforms(adapters map[string]registrytypes.ProviderPlatformAdapter) []string {
	platforms := make([]string, 0, len(adapters))
	for platform := range adapters {
		platforms = append(platforms, platform)
	}
	slices.Sort(platforms)
	return platforms
}

func normalizePlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}
