package v0

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

var errDeploymentNotSupported = errors.New("deployment operation is not supported for this provider platform type")

type localDeploymentAdapter struct {
	registry service.RegistryService
}

type kubernetesDeploymentAdapter struct {
	registry service.RegistryService
}

func (a *localDeploymentAdapter) Platform() string { return "local" }

func (a *localDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *localDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	if req == nil {
		return nil, fmt.Errorf("deployment request is required: %w", database.ErrInvalidInput)
	}
	if len(req.ProviderConfig) > 0 {
		return nil, fmt.Errorf("providerConfig is not supported for local deployments: %w", database.ErrInvalidInput)
	}
	providerID := req.ProviderID
	if providerID == "" {
		providerID = LocalProviderID
	}
	env := req.Env
	if env == nil {
		env = map[string]string{}
	}
	switch req.ResourceType {
	case "mcp":
		return a.registry.DeployServer(ctx, req.ServerName, req.Version, env, req.PreferRemote, providerID)
	case "agent":
		return a.registry.DeployAgent(ctx, req.ServerName, req.Version, env, req.PreferRemote, providerID)
	default:
		return nil, fmt.Errorf("invalid resource type %q: %w", req.ResourceType, database.ErrInvalidInput)
	}
}

func (a *localDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || deployment.ID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}
	return a.registry.RemoveDeploymentByID(ctx, deployment.ID)
}

func (a *localDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, errDeploymentNotSupported
}

func (a *localDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error {
	return errDeploymentNotSupported
}

func (a *localDeploymentAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return []*models.Deployment{}, nil
}

func (a *kubernetesDeploymentAdapter) Platform() string { return "kubernetes" }

func (a *kubernetesDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *kubernetesDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	if req == nil {
		return nil, fmt.Errorf("deployment request is required: %w", database.ErrInvalidInput)
	}
	if len(req.ProviderConfig) > 0 {
		return nil, fmt.Errorf("providerConfig is not supported for kubernetes deployments: %w", database.ErrInvalidInput)
	}
	providerID := req.ProviderID
	if providerID == "" {
		providerID = "kubernetes-default"
	}
	env := req.Env
	if env == nil {
		env = map[string]string{}
	}
	switch req.ResourceType {
	case "mcp":
		return a.registry.DeployServer(ctx, req.ServerName, req.Version, env, req.PreferRemote, providerID)
	case "agent":
		return a.registry.DeployAgent(ctx, req.ServerName, req.Version, env, req.PreferRemote, providerID)
	default:
		return nil, fmt.Errorf("invalid resource type %q: %w", req.ResourceType, database.ErrInvalidInput)
	}
}

func (a *kubernetesDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || deployment.ID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}
	return a.registry.RemoveDeploymentByID(ctx, deployment.ID)
}

func (a *kubernetesDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, errDeploymentNotSupported
}

func (a *kubernetesDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error {
	return errDeploymentNotSupported
}

func (a *kubernetesDeploymentAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return []*models.Deployment{}, nil
}

// DefaultDeploymentPlatformAdapters returns OSS deployment adapters for local and kubernetes.
func DefaultDeploymentPlatformAdapters(registry service.RegistryService) map[string]registrytypes.DeploymentPlatformAdapter {
	return map[string]registrytypes.DeploymentPlatformAdapter{
		"local":      &localDeploymentAdapter{registry: registry},
		"kubernetes": &kubernetesDeploymentAdapter{registry: registry},
	}
}
