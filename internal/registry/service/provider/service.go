package provider

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type Dependencies struct {
	StoreDB   database.Store
	Providers database.ProviderStore
}

type Service struct {
	providers database.ProviderStore
}

func New(deps Dependencies) *Service {
	providers := deps.Providers
	if providers == nil {
		providers = deps.StoreDB
	}

	return &Service{providers: providers}
}

func (s *Service) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.providers.ListProviders(ctx, platform)
}

func (s *Service) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.providers.GetProviderByID(ctx, providerID)
}

func (s *Service) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.providers.CreateProvider(ctx, in)
}

func (s *Service) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.providers.UpdateProvider(ctx, providerID, in)
}

func (s *Service) DeleteProvider(ctx context.Context, providerID string) error {
	return s.providers.DeleteProvider(ctx, providerID)
}
