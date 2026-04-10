package skill

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

const maxVersionsPerSkill = 10000

type Dependencies struct {
	StoreDB database.Store
	Skills  database.SkillStore
	Tx      database.Transactor
}

type Registry interface {
	database.SkillReader
	PublishSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error)
	DeleteSkill(ctx context.Context, skillName, version string) error
}

type registry struct {
	database.SkillStore
	tx database.Transactor
}

var _ Registry = (*registry)(nil)

func New(deps Dependencies) Registry {
	if deps.Skills == nil && deps.StoreDB != nil {
		deps.Skills = deps.StoreDB.Skills()
	}
	if deps.Tx == nil {
		deps.Tx = deps.StoreDB
	}

	return &registry{
		SkillStore: deps.Skills,
		tx:         deps.Tx,
	}
}

func (s *registry) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	return s.SkillStore.ListSkills(ctx, filter, cursor, limit)
}

func (s *registry) PublishSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return database.InTransactionT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*models.SkillResponse, error) {
		return s.createSkillInTransaction(txCtx, scope.Skills(), req)
	})
}

func (s *registry) DeleteSkill(ctx context.Context, skillName, version string) error {
	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Skills().DeleteSkill(txCtx, skillName, version)
	})
}

func (s *registry) createSkillInTransaction(ctx context.Context, skills database.SkillStore, req *models.SkillJSON) (*models.SkillResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid skill payload: name and version are required")
	}

	publishTime := time.Now()
	skillJSON := *req

	for _, remote := range skillJSON.Remotes {
		remoteURL := remote.URL
		filter := &database.SkillFilter{RemoteURL: &remoteURL}
		cursor := ""

		for {
			existing, nextCursor, err := skills.ListSkills(ctx, filter, cursor, 1000)
			if err != nil {
				return nil, fmt.Errorf("failed to check remote URL conflict: %w", err)
			}
			for _, existingSkill := range existing {
				if existingSkill.Skill.Name != skillJSON.Name {
					return nil, fmt.Errorf("remote URL %s is already used by skill %s", remoteURL, existingSkill.Skill.Name)
				}
			}
			if nextCursor == "" {
				break
			}
			cursor = nextCursor
		}
	}

	versionCount, err := skills.CountSkillVersions(ctx, skillJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxVersionsPerSkill {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := skills.CheckSkillVersionExists(ctx, skillJSON.Name, skillJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := skills.GetLatestSkill(ctx, skillJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		if versionutil.CompareVersions(skillJSON.Version, currentLatest.Skill.Version, publishTime, existingPublishedAt) <= 0 {
			isNewLatest = false
		}
	}

	if isNewLatest && currentLatest != nil {
		if err := skills.UnmarkSkillAsLatest(ctx, skillJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.SkillRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return skills.CreateSkill(ctx, &skillJSON, officialMeta)
}
