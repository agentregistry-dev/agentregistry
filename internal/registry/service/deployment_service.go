package service

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// DeploymentService defines deployment lifecycle operations.
type DeploymentService interface {
	// GetDeployments retrieves all deployed resources (MCP servers, agents)
	GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	// GetDeploymentByID retrieves a specific deployment by UUID.
	GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error)
	// DeployServer deploys an MCP server with configuration
	DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	// DeployAgent deploys an agent with configuration
	DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	// RemoveDeploymentByID removes a deployment by UUID.
	RemoveDeploymentByID(ctx context.Context, id string) error
	// CreateDeployment dispatches deployment creation via provider-resolved platform adapter.
	CreateDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	// UndeployDeployment dispatches undeploy via provider-resolved platform adapter.
	UndeployDeployment(ctx context.Context, deployment *models.Deployment) error
	// GetDeploymentLogs dispatches deployment log retrieval via provider-resolved platform adapter.
	GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error)
	// CancelDeployment dispatches deployment cancellation via provider-resolved platform adapter.
	CancelDeployment(ctx context.Context, deployment *models.Deployment) error
}
