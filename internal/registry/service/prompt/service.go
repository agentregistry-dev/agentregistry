package prompt

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	database.PromptReader
	PublishPrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error)
	DeletePrompt(ctx context.Context, promptName, version string) error
}

type registry struct {
	database.PromptStore
	tx database.Transactor
}

var _ Registry = (*registry)(nil)

func New(deps Dependencies) Registry {
	if deps.Prompts == nil && deps.StoreDB != nil {
		deps.Prompts = deps.StoreDB.Prompts()
	}
	if deps.Tx == nil {
		deps.Tx = deps.StoreDB
	}

	return &registry{
		PromptStore: deps.Prompts,
		tx:          deps.Tx,
	}
}

func (s *registry) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	return s.PromptStore.ListPrompts(ctx, filter, cursor, limit)
}

func (s *registry) PublishPrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return database.InTransactionT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*models.PromptResponse, error) {
		return s.createPromptInTransaction(txCtx, scope.Prompts(), req)
	})
}

func (s *registry) DeletePrompt(ctx context.Context, promptName, version string) error {
	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Prompts().DeletePrompt(txCtx, promptName, version)
	})
}

func (s *registry) createPromptInTransaction(ctx context.Context, prompts database.PromptStore, req *models.PromptJSON) (*models.PromptResponse, error) {
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

	currentLatest, err := prompts.GetLatestPrompt(ctx, promptJSON.Name)
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
