package v0

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

var errDeploymentNotSupported = errors.New("deployment operation is not supported for this provider platform type")

type deploymentAdapterBase struct {
	providerPlatform string
	registry         service.RegistryService
}

func (a *deploymentAdapterBase) Platform() string {
	return a.providerPlatform
}

func (a *deploymentAdapterBase) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *deploymentAdapterBase) Deploy(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	if req == nil {
		return nil, errors.New("deployment request is required")
	}
	resourceType := req.ResourceType
	if resourceType == "" {
		resourceType = "mcp"
	}
	if req.ProviderID == "" && a.providerPlatform == "local" {
		req.ProviderID = LocalProviderID
	}

	switch resourceType {
	case "mcp":
		return a.registry.DeployServer(ctx, req.ServerName, req.Version, req.Config, req.PreferRemote, req.ProviderID)
	case "agent":
		return a.registry.DeployAgent(ctx, req.ServerName, req.Version, req.Config, req.PreferRemote, req.ProviderID)
	default:
		return nil, errors.New("invalid resource type")
	}
}

func (a *deploymentAdapterBase) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || deployment.ID == "" {
		return errors.New("deployment id is required")
	}
	return a.registry.RemoveDeploymentByID(ctx, deployment.ID)
}

func (a *deploymentAdapterBase) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, errDeploymentNotSupported
}

func (a *deploymentAdapterBase) Cancel(_ context.Context, _ *models.Deployment) error {
	return errDeploymentNotSupported
}

func (a *deploymentAdapterBase) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	// Built-in local/kubernetes runtime discovery is handled through existing
	// reconciliation flows today; this adapter method is a no-op placeholder.
	return []*models.Deployment{}, nil
}

type localDeploymentAdapter struct {
	deploymentAdapterBase
}

type kubernetesDeploymentAdapter struct {
	deploymentAdapterBase
}

// NOTE: local and kubernetes currently share the same adapter base behavior.
// The registry service handles the runtime-specific paths today; keep these
// concrete adapter types as explicit extension points for future divergence.

// DefaultDeploymentPlatformAdapters returns OSS deployment adapters for local and kubernetes.
func DefaultDeploymentPlatformAdapters(registry service.RegistryService) map[string]registrytypes.DeploymentPlatformAdapter {
	return map[string]registrytypes.DeploymentPlatformAdapter{
		"local": &localDeploymentAdapter{
			deploymentAdapterBase: deploymentAdapterBase{
				providerPlatform: "local",
				registry:         registry,
			},
		},
		"kubernetes": &kubernetesDeploymentAdapter{
			deploymentAdapterBase: deploymentAdapterBase{
				providerPlatform: "kubernetes",
				registry:         registry,
			},
		},
	}
}
