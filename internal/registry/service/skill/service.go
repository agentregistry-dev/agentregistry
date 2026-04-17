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
	ApplySkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error)
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

func (s *registry) ApplySkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid skill payload: name and version are required")
	}
	return database.InTransactionT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*models.SkillResponse, error) {
		return s.applySkillInTransaction(txCtx, scope.Skills(), req)
	})
}

func (s *registry) applySkillInTransaction(ctx context.Context, skills database.SkillStore, req *models.SkillJSON) (*models.SkillResponse, error) {
	exists, err := skills.CheckSkillVersionExists(ctx, req.Name, req.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		if err := s.validateNoDuplicateRemoteURLs(ctx, skills, *req); err != nil {
			return nil, err
		}
		return skills.UpdateSkill(ctx, req.Name, req.Version, req)
	}
	return s.createSkillInTransaction(ctx, skills, req)
}

func (s *registry) DeleteSkill(ctx context.Context, skillName, version string) error {
	return database.InTransaction(ctx, s.tx, func(txCtx context.Context, scope database.Scope) error {
		return scope.Skills().DeleteSkill(txCtx, skillName, version)
	})
}

// validateNoDuplicateRemoteURLs ensures none of the requested remote URLs are
// already owned by a different skill. Used by both the create and apply paths
// to enforce the same uniqueness invariant.
func (s *registry) validateNoDuplicateRemoteURLs(ctx context.Context, skills database.SkillStore, skillDetail models.SkillJSON) error {
	for _, remote := range skillDetail.Remotes {
		remoteURL := remote.URL
		filter := &database.SkillFilter{RemoteURL: &remoteURL}
		cursor := ""

		for {
			conflictingSkills, nextCursor, err := skills.ListSkills(ctx, filter, cursor, 1000)
			if err != nil {
				return fmt.Errorf("failed to check remote URL conflict: %w", err)
			}
			for _, conflictingSkill := range conflictingSkills {
				if conflictingSkill.Skill.Name != skillDetail.Name {
					return fmt.Errorf("remote URL %s is already used by skill %s", remoteURL, conflictingSkill.Skill.Name)
				}
			}
			if nextCursor == "" {
				break
			}
			cursor = nextCursor
		}
	}
	return nil
}

func (s *registry) createSkillInTransaction(ctx context.Context, skills database.SkillStore, req *models.SkillJSON) (*models.SkillResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid skill payload: name and version are required")
	}

	publishTime := time.Now()
	skillJSON := *req

	if err := s.validateNoDuplicateRemoteURLs(ctx, skills, skillJSON); err != nil {
		return nil, err
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
