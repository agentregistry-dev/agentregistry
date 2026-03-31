package service

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// ListProviders lists providers, optionally filtered by platform.
func (s *providerServiceImpl) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.readStores().providers.ListProviders(ctx, platform)
}

// GetProviderByID gets a provider by ID.
func (s *providerServiceImpl) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.readStores().providers.GetProviderByID(ctx, providerID)
}

// CreateProvider creates a provider.
func (s *providerServiceImpl) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.readStores().providers.CreateProvider(ctx, in)
}

// UpdateProvider updates mutable provider fields.
func (s *providerServiceImpl) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.readStores().providers.UpdateProvider(ctx, providerID, in)
}

// DeleteProvider removes a provider by ID.
func (s *providerServiceImpl) DeleteProvider(ctx context.Context, providerID string) error {
	return s.readStores().providers.DeleteProvider(ctx, providerID)
}
