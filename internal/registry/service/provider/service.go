package provider

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type Dependencies struct {
	StoreDB database.Store
}

type Registry interface {
	CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error)
	ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error)
	GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error)
	UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error)
	DeleteProvider(ctx context.Context, providerID string) error
}

type Service struct {
	storeDB database.Store
}

var _ Registry = (*Service)(nil)

func New(deps Dependencies) Registry {
	return &Service{storeDB: deps.StoreDB}
}

func (s *Service) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.storeDB.CreateProvider(ctx, in)
}

func (s *Service) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.storeDB.ListProviders(ctx, platform)
}

func (s *Service) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.storeDB.GetProviderByID(ctx, providerID)
}

func (s *Service) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.storeDB.UpdateProvider(ctx, providerID, in)
}

func (s *Service) DeleteProvider(ctx context.Context, providerID string) error {
	return s.storeDB.DeleteProvider(ctx, providerID)
}