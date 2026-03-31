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

// SkillService defines skill catalog and mutation operations.
type SkillService interface {
	// ListSkills retrieve all skills with optional filtering
	ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	// GetSkillByName retrieve latest version of a skill by name
	GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error)
	// GetSkillByNameAndVersion retrieve specific version of a skill by name and version
	GetSkillByNameAndVersion(ctx context.Context, skillName string, version string) (*models.SkillResponse, error)
	// GetAllVersionsBySkillName retrieve all versions of a skill by name
	GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error)
	// CreateSkill creates a new skill version
	CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error)
	// DeleteSkill permanently removes a skill version from the registry
	DeleteSkill(ctx context.Context, skillName, version string) error
}

// ListSkills returns registry entries for skills with pagination and filtering.
func (s *skillServiceImpl) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if limit <= 0 {
		limit = 30
	}
	skills, next, err := s.readStores().skills.ListSkills(ctx, filter, cursor, limit)
	if err != nil {
		return nil, "", err
	}
	return skills, next, nil
}

// GetSkillByName retrieves the latest version of a skill by its name.
func (s *skillServiceImpl) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.readStores().skills.GetSkillByName(ctx, skillName)
}

// GetSkillByNameAndVersion retrieves a specific version of a skill by name and version.
func (s *skillServiceImpl) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.readStores().skills.GetSkillByNameAndVersion(ctx, skillName, version)
}

// GetAllVersionsBySkillName retrieves all versions for a skill.
func (s *skillServiceImpl) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.readStores().skills.GetAllVersionsBySkillName(ctx, skillName)
}

// CreateSkill creates a new skill version.
func (s *skillServiceImpl) CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return inTransactionT(ctx, s, func(ctx context.Context, stores storeBundle) (*models.SkillResponse, error) {
		return s.createSkillInTransaction(ctx, stores.skills, req)
	})
}

func (s *skillServiceImpl) createSkillInTransaction(ctx context.Context, skills database.SkillStore, req *models.SkillJSON) (*models.SkillResponse, error) {
	if req == nil || req.Name == "" || req.Version == "" {
		return nil, fmt.Errorf("invalid skill payload: name and version are required")
	}

	publishTime := time.Now()
	skillJSON := *req

	for _, remote := range skillJSON.Remotes {
		filter := &database.SkillFilter{RemoteURL: &remote.URL}
		existing, _, err := skills.ListSkills(ctx, filter, "", 1000)
		if err != nil {
			return nil, fmt.Errorf("failed to check remote URL conflict: %w", err)
		}
		for _, e := range existing {
			if e.Skill.Name != skillJSON.Name {
				return nil, fmt.Errorf("remote URL %s is already used by skill %s", remote.URL, e.Skill.Name)
			}
		}
	}

	versionCount, err := skills.CountSkillVersions(ctx, skillJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	if versionCount >= maxServerVersionsPerServer {
		return nil, database.ErrMaxVersionsReached
	}

	exists, err := skills.CheckSkillVersionExists(ctx, skillJSON.Name, skillJSON.Version)
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, database.ErrInvalidVersion
	}

	currentLatest, err := skills.GetCurrentLatestSkillVersion(ctx, skillJSON.Name)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}

	isNewLatest := true
	if currentLatest != nil {
		var existingPublishedAt time.Time
		if currentLatest.Meta.Official != nil {
			existingPublishedAt = currentLatest.Meta.Official.PublishedAt
		}
		if CompareVersions(skillJSON.Version, currentLatest.Skill.Version, publishTime, existingPublishedAt) <= 0 {
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

// DeleteSkill permanently removes a skill version from the registry.
func (s *skillServiceImpl) DeleteSkill(ctx context.Context, skillName, version string) error {
	return s.inTransaction(ctx, func(txCtx context.Context, stores storeBundle) error {
		return stores.skills.DeleteSkill(txCtx, skillName, version)
	})
}
