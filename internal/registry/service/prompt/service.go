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
	Prompts database.PromptStore
	Tx      database.Transactor
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
	prompts database.PromptStore
	tx      database.Transactor
}

var _ Registry = (*Service)(nil)

func New(deps Dependencies) Registry {
	if deps.Prompts == nil && deps.StoreDB != nil {
		deps.Prompts = deps.StoreDB.Prompts()
	}
	if deps.Tx == nil {
		deps.Tx = deps.StoreDB
	}

	return &Service{
		prompts: deps.Prompts,
		tx:      deps.Tx,
	}
}

func (s *Service) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	return s.prompts.ListPrompts(ctx, filter, cursor, limit)
}

func (s *Service) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.prompts.GetPromptByName(ctx, promptName)
}

func (s *Service) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.prompts.GetPromptByNameAndVersion(ctx, promptName, version)
}

func (s *Service) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.prompts.GetAllVersionsByPromptName(ctx, promptName)
}

func (s *Service) CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return txutil.RunT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*models.PromptResponse, error) {
		return s.createPromptInTransaction(txCtx, scope.Prompts(), req)
	})
}

func (s *Service) DeletePrompt(ctx context.Context, promptName, version string) error {
	return txutil.Run(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Prompts().DeletePrompt(txCtx, promptName, version)
	})
}

func (s *Service) createPromptInTransaction(ctx context.Context, prompts database.PromptStore, req *models.PromptJSON) (*models.PromptResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid prompt payload: name and version are required")
	}

	publishTime := time.Now()
	promptJSON := *req

	versionCount, err := prompts.CountPromptVersions(ctx, promptJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxVersionsPerPrompt {
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
		if versionutil.CompareVersions(promptJSON.Version, currentLatest.Prompt.Version, publishTime, existingPublishedAt) <= 0 {
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
