package kubernetes

import (
	"context"
	"fmt"
	"log"
	"strings"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type kubernetesDeploymentAdapter struct {
	providerRegistry providersvc.Registry
	serverService    serversvc.Registry
	agentService     agentsvc.Registry
}

func NewKubernetesDeploymentAdapter(providerRegistry providersvc.Registry, serverService serversvc.Registry, agentService agentsvc.Registry) *kubernetesDeploymentAdapter {
	return &kubernetesDeploymentAdapter{providerRegistry: providerRegistry, serverService: serverService, agentService: agentService}
}

func (a *kubernetesDeploymentAdapter) Platform() string { return "kubernetes" }

func (a *kubernetesDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *kubernetesDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	if err := utils.ValidateDeploymentRequest(req, false); err != nil {
		return nil, err
	}

	provider, err := a.providerRegistry.GetProvider(ctx, req.ProviderID)
	if err != nil {
		return nil, err
	}

	cfg, err := a.translateKubernetesDeployment(ctx, req, provider)
	if err != nil {
		return nil, err
	}
	if err := kubernetesApplyPlatformConfig(ctx, provider, cfg, false); err != nil {
		return nil, fmt.Errorf("apply kubernetes platform config: %w", err)
	}
	return &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}, nil
}

func (a *kubernetesDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if err := utils.ValidateDeploymentRequest(deployment, true); err != nil {
		return err
	}
	provider, err := a.providerRegistry.GetProvider(ctx, deployment.ProviderID)
	if err != nil {
		return err
	}
	namespace := deploymentNamespace(deployment, provider)
	return kubernetesDeleteResourcesByDeploymentID(ctx, provider, deployment.ID, strings.ToLower(strings.TrimSpace(deployment.ResourceType)), namespace)
}

func (a *kubernetesDeploymentAdapter) CleanupStale(ctx context.Context, deployment *models.Deployment) error {
	if err := utils.ValidateDeploymentRequest(deployment, true); err != nil {
		return err
	}
	provider, err := a.providerRegistry.GetProvider(ctx, deployment.ProviderID)
	if err != nil {
		return err
	}
	if err := kubernetesDeleteResourcesByDeploymentID(ctx, provider, deployment.ID, strings.ToLower(strings.TrimSpace(deployment.ResourceType)), deploymentNamespace(deployment, provider)); err != nil {
		log.Printf("Warning: failed to clean up stale kubernetes deployment %s: %v", deployment.ID, err)
	}
	return nil
}

func (a *kubernetesDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, utils.ErrDeploymentNotSupported
}

func (a *kubernetesDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error {
	return utils.ErrDeploymentNotSupported
}

func (a *kubernetesDeploymentAdapter) Discover(ctx context.Context, providerID string) ([]*models.Deployment, error) {
	provider, err := a.providerRegistry.GetProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	return kubernetesDiscoverDeployments(ctx, provider)
}

func (a *kubernetesDeploymentAdapter) translateKubernetesDeployment(
	ctx context.Context,
	deployment *models.Deployment,
	provider *models.Provider,
) (*platformtypes.KubernetesPlatformConfig, error) {
	desired, err := a.buildKubernetesDesiredState(ctx, deployment, provider)
	if err != nil {
		return nil, err
	}
	cfg, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("translate kubernetes platform config: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("kubernetes platform config is required")
	}
	return cfg, nil
}

func (a *kubernetesDeploymentAdapter) buildKubernetesDesiredState(
	ctx context.Context,
	deployment *models.Deployment,
	provider *models.Provider,
) (*platformtypes.DesiredState, error) {
	namespace := deploymentNamespace(deployment, provider)
	resourceType := strings.ToLower(strings.TrimSpace(deployment.ResourceType))
	switch resourceType {
	case "mcp":
		server, err := utils.BuildPlatformMCPServer(ctx, a.serverService, deployment, namespace)
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{MCPServers: []*platformtypes.MCPServer{server}}, nil
	case "agent":
		resolved, err := utils.ResolveAgent(ctx, a.serverService, a.agentService, deployment, namespace)
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{
			Agents:     []*platformtypes.Agent{resolved.Agent},
			MCPServers: resolved.ResolvedPlatformServers,
		}, nil
	default:
		return nil, fmt.Errorf("invalid resource type %q: %w", deployment.ResourceType, database.ErrInvalidInput)
	}
}

func deploymentNamespace(deployment *models.Deployment, provider *models.Provider) string {
	if deployment != nil && deployment.Env != nil {
		if namespace := strings.TrimSpace(deployment.Env["KAGENT_NAMESPACE"]); namespace != "" {
			return namespace
		}
	}
	if namespace := kubernetesProviderNamespace(provider); namespace != "" {
		return namespace
	}
	return kubernetesDefaultNamespace()
}
