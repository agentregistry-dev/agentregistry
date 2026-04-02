package service

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
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
