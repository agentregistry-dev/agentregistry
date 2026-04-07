package skill

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

const maxVersionsPerSkill = 10000

type Dependencies struct {
	StoreDB database.Store
}

type Registry interface {
	ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error)
	GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error)
	GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error)
	CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error)
	DeleteSkill(ctx context.Context, skillName, version string) error
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

func (s *Service) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	return s.storeDB.ListSkills(ctx, filter, cursor, limit)
}

func (s *Service) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.storeDB.GetSkillByName(ctx, skillName)
}

func (s *Service) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.storeDB.GetSkillByNameAndVersion(ctx, skillName, version)
}

func (s *Service) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.storeDB.GetAllVersionsBySkillName(ctx, skillName)
}

func (s *Service) CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return txutil.RunT(ctx, s.storeDB, func(txCtx context.Context, store database.Store) (*models.SkillResponse, error) {
		return s.createSkillInTransaction(txCtx, store, req)
	})
}

func (s *Service) DeleteSkill(ctx context.Context, skillName, version string) error {
	return txutil.Run(ctx, s.storeDB, func(txCtx context.Context, store database.Store) error {
		return store.DeleteSkill(txCtx, skillName, version)
	})
}

func (s *Service) createSkillInTransaction(ctx context.Context, store database.Store, req *models.SkillJSON) (*models.SkillResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid skill payload: name and version are required")
	}

	publishTime := time.Now()
	skillJSON := *req

	for _, remote := range skillJSON.Remotes {
		filter := &database.SkillFilter{RemoteURL: &remote.URL}
		existing, _, err := store.ListSkills(ctx, filter, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to check remote URL conflict: %w", err)
		}
		for _, existingSkill := range existing {
			if existingSkill.Skill.Name != skillJSON.Name {
				return nil, fmt.Errorf("remote URL %s is already used by skill %s", remote.URL, existingSkill.Skill.Name)
			}
		}
	}

	versionCount, err := store.CountSkillVersions(ctx, skillJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxVersionsPerSkill {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := store.CheckSkillVersionExists(ctx, skillJSON.Name, skillJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := store.GetCurrentLatestSkillVersion(ctx, skillJSON.Name)
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
		if err := store.UnmarkSkillAsLatest(ctx, skillJSON.Name); err != nil {
			return nil, err
		}
	}

	officialMeta := &models.SkillRegistryExtensions{
		Status:      string(model.StatusActive),
		PublishedAt: publishTime,
		UpdatedAt:   publishTime,
		IsLatest:    isNewLatest,
	}

	return store.CreateSkill(ctx, &skillJSON, officialMeta)
}
