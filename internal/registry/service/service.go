package service

import (
	"context"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// MCPRegistryService defines the read-heavy and deployment operations needed by the MCP bridge.
type MCPRegistryService interface {
	ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error)
	GetAgentByNameAndVersion(ctx context.Context, agentName string, version string) (*models.AgentResponse, error)

	ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error)
	GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error)
	GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error)

	ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error)
	GetSkillByNameAndVersion(ctx context.Context, skillName string, version string) (*models.SkillResponse, error)

	GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error)
	DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	UndeployDeployment(ctx context.Context, deployment *models.Deployment) error
}

// PlatformRuntimeService defines the registry operations needed to materialize platform deployments.
type PlatformRuntimeService interface {
	GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error)
	GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error)
	GetAgentByNameAndVersion(ctx context.Context, agentName string, version string) (*models.AgentResponse, error)
	ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error)
	ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error)
}

// APIRouteService defines the registry operations needed by the HTTP API routing layer.
type APIRouteService interface {
	ServerService
	AgentService
	SkillService
	PromptService
	ProviderService
	DeploymentService
}

// RegistryService defines the interface for registry operations
type RegistryService interface {
	ServerService
	AgentService
	SkillService
	PromptService

	ProviderService
	DeploymentService
}
