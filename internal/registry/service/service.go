package service

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Reconciler handles server-side reconciliation of deployed resources (MCP servers, agents)
type Reconciler interface {
	ReconcileAll(ctx context.Context) error
}

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// ListServers retrieve all servers with optional filtering
	ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// GetServerByName retrieve latest version of a server by server name
	GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	// GetServerByNameAndVersion retrieve specific version of a server by server name and version
	GetServerByNameAndVersion(ctx context.Context, serverName string, version string, publishedOnly bool) (*apiv0.ServerResponse, error)
	// GetAllVersionsByServerName retrieve all versions of a server by server name
	GetAllVersionsByServerName(ctx context.Context, serverName string, publishedOnly bool) ([]*apiv0.ServerResponse, error)
	// CreateServer creates a new server version
	CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// UpdateServer updates an existing server and optionally its status
	UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error)
	// StoreServerReadme stores or updates the README for a server version
	StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error
	// GetServerReadmeLatest retrieves the README for the latest server version
	GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error)
	// GetServerReadmeByVersion retrieves the README for a specific server version
	GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error)
	// PublishServer marks a server as published
	PublishServer(ctx context.Context, serverName, version string) error
	// UnpublishServer marks a server as unpublished
	UnpublishServer(ctx context.Context, serverName, version string) error
	// DeleteServer permanently removes a server version from the registry
	DeleteServer(ctx context.Context, serverName, version string) error

	// Agents APIs
	// ListAgents retrieve all agents with optional filtering
	ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	// GetAgentByName retrieve latest version of an agent by name
	GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error)
	// GetAgentByNameAndVersion retrieve specific version of an agent by name and version
	GetAgentByNameAndVersion(ctx context.Context, agentName string, version string) (*models.AgentResponse, error)
	// GetAllVersionsByAgentName retrieve all versions of an agent by name
	GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error)
	// CreateAgent creates a new agent version
	CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	// PublishAgent marks an agent as published
	PublishAgent(ctx context.Context, agentName, version string) error
	// UnpublishAgent marks an agent as unpublished
	UnpublishAgent(ctx context.Context, agentName, version string) error
	// DeleteAgent permanently removes an agent version from the registry
	DeleteAgent(ctx context.Context, agentName, version string) error
	// Skills APIs
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
	// PublishSkill marks a skill as published
	PublishSkill(ctx context.Context, skillName, version string) error
	// UnpublishSkill marks a skill as unpublished
	UnpublishSkill(ctx context.Context, skillName, version string) error

	// Deployments APIs
	// GetDeployments retrieves all deployed resources (MCP servers, agents)
	GetDeployments(ctx context.Context) ([]*models.Deployment, error)
	// GetDeploymentByName retrieves a specific deployment by resource name
	GetDeploymentByNameAndVersion(ctx context.Context, resourceName string, version string) (*models.Deployment, error)
	// DeployServer deploys an MCP server with configuration
	DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool) (*models.Deployment, error)
	// DeployAgent deploys an agent with configuration (to be implemented)
	DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool) (*models.Deployment, error)
	// UpdateDeploymentConfig updates the configuration for a deployment
	UpdateDeploymentConfig(ctx context.Context, resourceName string, version string, config map[string]string) (*models.Deployment, error)
	// RemoveServer removes a deployment (works for any resource type)
	RemoveServer(ctx context.Context, resourceName string, version string) error

	Reconciler
}
