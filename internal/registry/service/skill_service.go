package service

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
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
