package types

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// PlatformRuntimeService defines the registry operations consumed by platform materialization.
type PlatformRuntimeService interface {
	GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error)
	GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error)
	GetAgentByNameAndVersion(ctx context.Context, agentName string, version string) (*models.AgentResponse, error)
	ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]AgentSkillRef, error)
	ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]ResolvedPrompt, error)
}
