package v0

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

type providerAdapterBase struct {
	providerPlatform string
	registry         service.RegistryService
}

func (a *providerAdapterBase) Platform() string {
	return a.providerPlatform
}

func (a *providerAdapterBase) ListProviders(ctx context.Context) ([]*models.Provider, error) {
	platform := a.providerPlatform
	return a.registry.ListProviders(ctx, &platform)
}

func (a *providerAdapterBase) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	if in == nil {
		return nil, database.ErrInvalidInput
	}
	if in.Platform != a.providerPlatform {
		return nil, database.ErrInvalidInput
	}
	return a.registry.CreateProvider(ctx, in)
}

func (a *providerAdapterBase) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	provider, err := a.registry.GetProviderByID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if provider == nil || provider.Platform != a.providerPlatform {
		return nil, database.ErrNotFound
	}
	return provider, nil
}

func (a *providerAdapterBase) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	provider, err := a.GetProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	updated, err := a.registry.UpdateProvider(ctx, provider.ID, in)
	if err != nil {
		return nil, err
	}
	if updated.Platform != a.providerPlatform {
		return nil, errors.New("updated provider platform mismatch")
	}
	return updated, nil
}

func (a *providerAdapterBase) DeleteProvider(ctx context.Context, providerID string) error {
	if _, err := a.GetProvider(ctx, providerID); err != nil {
		return err
	}
	return a.registry.DeleteProvider(ctx, providerID)
}

type localProviderAdapter struct {
	providerAdapterBase
}

type kubernetesProviderAdapter struct {
	providerAdapterBase
}

// NOTE: local and kubernetes currently share the same adapter base behavior.
// Provider CRUD remains extension-driven, and these concrete adapter types are
// kept explicit so platform-specific validation can diverge later if needed.

// DefaultProviderPlatformAdapters returns OSS provider adapters for local and kubernetes.
func DefaultProviderPlatformAdapters(registry service.RegistryService) map[string]registrytypes.ProviderPlatformAdapter {
	return map[string]registrytypes.ProviderPlatformAdapter{
		"local": &localProviderAdapter{
			providerAdapterBase: providerAdapterBase{
				providerPlatform: "local",
				registry:         registry,
			},
		},
		"kubernetes": &kubernetesProviderAdapter{
			providerAdapterBase: providerAdapterBase{
				providerPlatform: "kubernetes",
				registry:         registry,
			},
		},
	}
}
