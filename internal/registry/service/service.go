package service

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	agentmodels "github.com/agentregistry-dev/agentregistry/internal/registry/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// RegistryService defines the interface for registry operations
type RegistryService interface {
	// ListServers retrieve all servers with optional filtering
	ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	// GetServerByName retrieve latest version of a server by server name
	GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	// GetServerByNameAndVersion retrieve specific version of a server by server name and version
	GetServerByNameAndVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error)
	// GetAllVersionsByServerName retrieve all versions of a server by server name
	GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	// CreateServer creates a new server version
	CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	// UpdateServer updates an existing server and optionally its status
	UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error)

	// Agents APIs
	// ListAgents retrieve all agents with optional filtering
	ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*agentmodels.AgentResponse, string, error)
	// GetAgentByName retrieve latest version of an agent by name
	GetAgentByName(ctx context.Context, agentName string) (*agentmodels.AgentResponse, error)
	// GetAgentByNameAndVersion retrieve specific version of an agent by name and version
	GetAgentByNameAndVersion(ctx context.Context, agentName string, version string) (*agentmodels.AgentResponse, error)
	// GetAllVersionsByAgentName retrieve all versions of an agent by name
	GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*agentmodels.AgentResponse, error)
	// CreateAgent creates a new agent version
	CreateAgent(ctx context.Context, req *agentmodels.AgentJSON) (*agentmodels.AgentResponse, error)
}
