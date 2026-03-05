package v0

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var errDeploymentNotSupported = errors.New("deployment operation is not supported for this provider platform type")

const (
	defaultKubernetesProviderID = "kubernetes-default"
	managedLabelKey             = "aregistry.ai/managed"
	managedLabelValue           = "true"
)

type localDeploymentAdapter struct {
}

type kubernetesDeploymentAdapter struct {
}

// DefaultDeploymentAdapterConfig configures built-in deployment adapters.
type DefaultDeploymentAdapterConfig struct {
	RuntimeDir       string
	AgentGatewayPort uint16
}

func (a *localDeploymentAdapter) Platform() string { return "local" }

func (a *localDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *localDeploymentAdapter) Deploy(_ context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	if req == nil {
		return nil, fmt.Errorf("deployment request is required: %w", database.ErrInvalidInput)
	}
	if len(req.ProviderConfig) > 0 {
		return nil, fmt.Errorf("providerConfig is not supported for local deployments: %w", database.ErrInvalidInput)
	}
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("provider id is required: %w", database.ErrInvalidInput)
	}
	switch req.ResourceType {
	case "mcp":
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	case "agent":
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	default:
		return nil, fmt.Errorf("invalid resource type %q: %w", req.ResourceType, database.ErrInvalidInput)
	}
}

func (a *localDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || deployment.ID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}
	return nil
}

func (a *localDeploymentAdapter) CleanupStale(ctx context.Context, deployment *models.Deployment) error {
	// Local stale deployment replacement only needs DB row cleanup.
	return nil
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

func (a *kubernetesDeploymentAdapter) Deploy(_ context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	if req == nil {
		return nil, fmt.Errorf("deployment request is required: %w", database.ErrInvalidInput)
	}
	if len(req.ProviderConfig) > 0 {
		return nil, fmt.Errorf("providerConfig is not supported for kubernetes deployments: %w", database.ErrInvalidInput)
	}
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("provider id is required: %w", database.ErrInvalidInput)
	}
	switch req.ResourceType {
	case "mcp":
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	case "agent":
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	default:
		return nil, fmt.Errorf("invalid resource type %q: %w", req.ResourceType, database.ErrInvalidInput)
	}
}

func (a *kubernetesDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || deployment.ID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}
	if err := cleanupKubernetesDeploymentResources(ctx, deployment); err != nil {
		return err
	}
	return nil
}

func (a *kubernetesDeploymentAdapter) CleanupStale(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || deployment.ID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}
	// Best-effort stale cleanup: resources may already be gone.
	if err := cleanupKubernetesDeploymentResources(ctx, deployment); err != nil {
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
	appendResource := func(
		resType, name string,
		labels map[string]string,
		creation time.Time,
		_ []metav1.Condition,
	) {
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

	agents, err := runtime.ListAgents(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to list kubernetes agents for discovery: %v", err)
	} else {
		for _, agent := range agents {
			appendResource("agent", agent.Name, agent.Labels, agent.CreationTimestamp.Time, agent.Status.Conditions)
		}
	}

	mcpServers, err := runtime.ListMCPServers(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to list kubernetes MCP servers for discovery: %v", err)
	} else {
		for _, mcp := range mcpServers {
			appendResource("mcpserver", mcp.Name, mcp.Labels, mcp.CreationTimestamp.Time, mcp.Status.Conditions)
		}
	}

	remoteMCPs, err := runtime.ListRemoteMCPServers(ctx, "")
	if err != nil {
		log.Printf("Warning: failed to list kubernetes remote MCP servers for discovery: %v", err)
	} else {
		for _, remote := range remoteMCPs {
			appendResource("remotemcpserver", remote.Name, remote.Labels, remote.CreationTimestamp.Time, remote.Status.Conditions)
		}
	}

	return discovered, nil
}

func cleanupKubernetesDeploymentResources(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || strings.TrimSpace(deployment.ID) == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}

	namespace := ""
	if deployment.Env != nil {
		namespace = deployment.Env["KAGENT_NAMESPACE"]
	}
	if namespace == "" {
		namespace = runtime.DefaultNamespace()
	}

	return runtime.DeleteKubernetesResourcesByDeploymentID(ctx, deployment.ID, deployment.ResourceType, namespace)
}

// DefaultDeploymentPlatformAdapters returns OSS deployment adapters for local and kubernetes.
func DefaultDeploymentPlatformAdapters(
	_ service.RegistryService,
	cfg ...DefaultDeploymentAdapterConfig,
) map[string]registrytypes.DeploymentPlatformAdapter {
	_ = cfg

	return map[string]registrytypes.DeploymentPlatformAdapter{
		"local":      &localDeploymentAdapter{},
		"kubernetes": &kubernetesDeploymentAdapter{},
	}
}
