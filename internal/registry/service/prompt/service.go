package prompt

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/txutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/versionutil"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const maxVersionsPerPrompt = 10000

type Dependencies struct {
	StoreDB database.Store
}

type Registry interface {
	ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error)
	GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error)
	GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error)
	GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error)
	CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error)
	DeletePrompt(ctx context.Context, promptName, version string) error
}

type Service struct {
	storeDB database.Store
}

var _ Registry = (*Service)(nil)

func New(deps Dependencies) Registry {
	return &Service{
		storeDB: deps.StoreDB,
	}
}

func (s *Service) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	return s.storeDB.ListPrompts(ctx, filter, cursor, limit)
}

func (s *Service) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.storeDB.GetPromptByName(ctx, promptName)
}

func (s *Service) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.storeDB.GetPromptByNameAndVersion(ctx, promptName, version)
}

func (s *Service) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.storeDB.GetAllVersionsByPromptName(ctx, promptName)
}

func (s *Service) CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return txutil.RunT(ctx, s.storeDB, func(txCtx context.Context, store database.Store) (*models.PromptResponse, error) {
		return s.createPromptInTransaction(txCtx, store, req)
	})
}

func (s *Service) DeletePrompt(ctx context.Context, promptName, version string) error {
	return txutil.Run(ctx, s.storeDB, func(txCtx context.Context, store database.Store) error {
		return store.DeletePrompt(txCtx, promptName, version)
	})
}

func (s *Service) createPromptInTransaction(ctx context.Context, store database.Store, req *models.PromptJSON) (*models.PromptResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid prompt payload: name and version are required")
	}

	publishTime := time.Now()
	promptJSON := *req

	versionCount, err := store.CountPromptVersions(ctx, promptJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxVersionsPerPrompt {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := store.CheckPromptVersionExists(ctx, promptJSON.Name, promptJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := store.GetCurrentLatestPromptVersion(ctx, promptJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		if versionutil.CompareVersions(promptJSON.Version, currentLatest.Prompt.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := store.UnmarkPromptAsLatest(ctx, promptJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.PromptRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return store.CreatePrompt(ctx, &promptJSON, officialMeta)
}
