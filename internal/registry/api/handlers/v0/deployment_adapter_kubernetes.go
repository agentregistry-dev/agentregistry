package v0

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	kubernetesplatform "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/kubernetes"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type kubernetesDeploymentAdapter struct {
	registry service.RegistryService
}

func (a *kubernetesDeploymentAdapter) Platform() string { return "kubernetes" }

func (a *kubernetesDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *kubernetesDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	if err := validateAdapterDeploymentRequest(req, false); err != nil {
		return nil, err
	}

	cfg, err := a.translateKubernetesDeployment(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := kubernetesplatform.ApplyPlatformConfig(ctx, cfg, false); err != nil {
		return nil, fmt.Errorf("apply kubernetes platform config: %w", err)
	}
	return &models.DeploymentActionResult{Status: "deployed"}, nil
}

func (a *kubernetesDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if err := validateAdapterDeploymentRequest(deployment, true); err != nil {
		return err
	}
	namespace := deploymentNamespace(deployment)
	return kubernetesplatform.DeleteResourcesByDeploymentID(ctx, deployment.ID, strings.ToLower(strings.TrimSpace(deployment.ResourceType)), namespace)
}

func (a *kubernetesDeploymentAdapter) CleanupStale(ctx context.Context, deployment *models.Deployment) error {
	if err := validateAdapterDeploymentRequest(deployment, true); err != nil {
		return err
	}
	if err := kubernetesplatform.DeleteResourcesByDeploymentID(ctx, deployment.ID, strings.ToLower(strings.TrimSpace(deployment.ResourceType)), deploymentNamespace(deployment)); err != nil {
		log.Printf("Warning: failed to clean up stale kubernetes deployment %s: %v", deployment.ID, err)
	}
	return nil
}

func (a *kubernetesDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, errDeploymentNotSupported
}

func (a *kubernetesDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error {
	return errDeploymentNotSupported
}

func (a *kubernetesDeploymentAdapter) Discover(ctx context.Context, providerID string) ([]*models.Deployment, error) {
	provider := strings.TrimSpace(providerID)
	if provider == "" {
		provider = defaultKubernetesProviderID
	}

	isManaged := func(labels map[string]string) bool {
		return labels != nil && labels[managedLabelKey] == managedLabelValue
	}

	discovered := make([]*models.Deployment, 0)
	appendResource := func(resType, name string, labels map[string]string, creation time.Time) {
		if isManaged(labels) {
			return
		}

		resourceType := "agent"
		if resType == "mcpserver" || resType == "remotemcpserver" {
			resourceType = "mcp"
		}

		preferRemote := resType == "remotemcpserver"
		meta, _ := models.UnmarshalFrom(models.KubernetesProviderMetadata{IsExternal: true})
		discovered = append(discovered, &models.Deployment{
			ServerName:       name,
			Version:          "unknown",
			DeployedAt:       creation,
			UpdatedAt:        creation,
			Status:           "deployed",
			Origin:           "discovered",
			ProviderID:       provider,
			ResourceType:     resourceType,
			PreferRemote:     preferRemote,
			Env:              labels,
			ProviderMetadata: meta,
		})
	}

	agents, err := kubernetesplatform.ListAgents(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to list kubernetes agents for discovery: %v", err)
	} else {
		for _, agent := range agents {
			appendResource("agent", agent.Name, agent.Labels, agent.CreationTimestamp.Time)
		}
	}

	mcpServers, err := kubernetesplatform.ListMCPServers(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to list kubernetes MCP servers for discovery: %v", err)
	} else {
		for _, mcp := range mcpServers {
			appendResource("mcpserver", mcp.Name, mcp.Labels, mcp.CreationTimestamp.Time)
		}
	}

	remoteMCPs, err := kubernetesplatform.ListRemoteMCPServers(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to list kubernetes remote MCP servers for discovery: %v", err)
	} else {
		for _, remote := range remoteMCPs {
			appendResource("remotemcpserver", remote.Name, remote.Labels, remote.CreationTimestamp.Time)
		}
	}

	return discovered, nil
}

func (a *kubernetesDeploymentAdapter) translateKubernetesDeployment(
	ctx context.Context,
	deployment *models.Deployment,
) (*platformtypes.KubernetesPlatformConfig, error) {
	desired, err := a.buildKubernetesDesiredState(ctx, deployment)
	if err != nil {
		return nil, err
	}
	cfg, err := kubernetesplatform.TranslatePlatformConfig(ctx, desired)
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
) (*platformtypes.DesiredState, error) {
	namespace := deploymentNamespace(deployment)
	resourceType := strings.ToLower(strings.TrimSpace(deployment.ResourceType))
	switch resourceType {
	case "mcp":
		server, err := buildPlatformMCPServer(ctx, a.registry, deployment, namespace)
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{MCPServers: []*platformtypes.MCPServer{server}}, nil
	case "agent":
		materialized, err := buildKubernetesAgentMaterialization(ctx, a.registry, deployment, namespace)
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{
			Agents:     []*platformtypes.Agent{materialized.agent},
			MCPServers: materialized.resolvedPlatformServers,
		}, nil
	default:
		return nil, fmt.Errorf("invalid resource type %q: %w", deployment.ResourceType, database.ErrInvalidInput)
	}
}
