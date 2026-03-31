package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// PromptService defines prompt catalog and mutation operations.
type PromptService interface {
	// ListPrompts retrieve all prompts with optional filtering
	ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error)
	// GetPromptByName retrieve latest version of a prompt by name
	GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error)
	// GetPromptByNameAndVersion retrieve specific version of a prompt by name and version
	GetPromptByNameAndVersion(ctx context.Context, promptName string, version string) (*models.PromptResponse, error)
	// GetAllVersionsByPromptName retrieve all versions of a prompt by name
	GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error)
	// CreatePrompt creates a new prompt version
	CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error)
	// DeletePrompt permanently removes a prompt version from the registry
	DeletePrompt(ctx context.Context, promptName, version string) error
}

// ListPrompts returns registry entries for prompts with pagination and filtering.
func (s *promptServiceImpl) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	prompts, next, err := s.readStores().prompts.ListPrompts(ctx, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	return prompts, next, nil
}

// GetPromptByName retrieves the latest version of a prompt by its name.
func (s *promptServiceImpl) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.readStores().prompts.GetPromptByName(ctx, promptName)
}

// GetPromptByNameAndVersion retrieves a specific version of a prompt by name and version.
func (s *promptServiceImpl) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.readStores().prompts.GetPromptByNameAndVersion(ctx, promptName, version)
}

// GetAllVersionsByPromptName retrieves all versions for a prompt.
func (s *promptServiceImpl) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.readStores().prompts.GetAllVersionsByPromptName(ctx, promptName)
}

// CreatePrompt creates a new prompt version.
func (s *promptServiceImpl) CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return inTransactionT(ctx, s, func(ctx context.Context, stores storeBundle) (*models.PromptResponse, error) {
		return s.createPromptInTransaction(ctx, stores.prompts, req)
	})
}

func (s *promptServiceImpl) createPromptInTransaction(ctx context.Context, prompts database.PromptStore, req *models.PromptJSON) (*models.PromptResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid prompt payload: name and version are required")
	}

	publishTime := time.Now()
	promptJSON := *req

	versionCount, err := prompts.CountPromptVersions(ctx, promptJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := prompts.CheckPromptVersionExists(ctx, promptJSON.Name, promptJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := prompts.GetCurrentLatestPromptVersion(ctx, promptJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		if CompareVersions(promptJSON.Version, currentLatest.Prompt.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := prompts.UnmarkPromptAsLatest(ctx, promptJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.PromptRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return prompts.CreatePrompt(ctx, &promptJSON, officialMeta)
}

// DeletePrompt permanently removes a prompt version from the registry.
func (s *promptServiceImpl) DeletePrompt(ctx context.Context, promptName, version string) error {
	return s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		return stores.prompts.DeletePrompt(txCtx, promptName, version)
	})
}
