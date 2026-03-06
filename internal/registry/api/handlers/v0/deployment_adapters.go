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
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/kagent"
	runtimeregistry "github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
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
	registry         service.RegistryService
	runtimeDir       string
	agentGatewayPort uint16
}

type kubernetesDeploymentAdapter struct {
	registry service.RegistryService
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

func (a *localDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
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
		if err := a.reconcileProviderDeployments(ctx, providerID, ""); err != nil {
			return nil, err
		}
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	case "agent":
		if err := a.reconcileProviderDeployments(ctx, providerID, ""); err != nil {
			return nil, err
		}
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	default:
		return nil, fmt.Errorf("invalid resource type %q: %w", req.ResourceType, database.ErrInvalidInput)
	}
}

func (a *localDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil || deployment.ID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}
	return a.reconcileProviderDeployments(ctx, deployment.ProviderID, deployment.ID)
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

func (a *localDeploymentAdapter) reconcileProviderDeployments(ctx context.Context, providerID, excludeDeploymentID string) error {
	servers, agents, err := buildRuntimeRequestsForProvider(ctx, a.registry, providerID, excludeDeploymentID)
	if err != nil {
		return err
	}
	agentRuntime := runtime.NewAgentRegistryRuntime(
		runtimeregistry.NewTranslator(),
		dockercompose.NewAgentGatewayTranslator(a.runtimeDir, a.agentGatewayPort),
		a.runtimeDir,
		false,
	)
	if err := agentRuntime.ReconcileAll(ctx, servers, agents); err != nil {
		return fmt.Errorf("failed to reconcile local runtime: %w", err)
	}
	return nil
}

func (a *kubernetesDeploymentAdapter) Platform() string { return "kubernetes" }

func (a *kubernetesDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *kubernetesDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
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
		if err := a.reconcileProviderDeployments(ctx, providerID); err != nil {
			return nil, err
		}
		return &models.DeploymentActionResult{Status: "deployed"}, nil
	case "agent":
		if err := a.reconcileProviderDeployments(ctx, providerID); err != nil {
			return nil, err
		}
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

func (a *kubernetesDeploymentAdapter) reconcileProviderDeployments(ctx context.Context, providerID string) error {
	servers, agents, err := buildRuntimeRequestsForProvider(ctx, a.registry, providerID, "")
	if err != nil {
		return err
	}
	agentRuntime := runtime.NewAgentRegistryRuntime(
		runtimeregistry.NewTranslator(),
		kagent.NewTranslator(),
		"",
		false,
	)
	if err := agentRuntime.ReconcileAll(ctx, servers, agents); err != nil {
		return fmt.Errorf("failed to reconcile kubernetes runtime: %w", err)
	}
	return nil
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
	registry service.RegistryService,
	cfg ...DefaultDeploymentAdapterConfig,
) map[string]registrytypes.DeploymentPlatformAdapter {
	settings := DefaultDeploymentAdapterConfig{}
	if len(cfg) > 0 {
		settings = cfg[0]
	}

	return map[string]registrytypes.DeploymentPlatformAdapter{
		"local": &localDeploymentAdapter{
			registry:         registry,
			runtimeDir:       settings.RuntimeDir,
			agentGatewayPort: settings.AgentGatewayPort,
		},
		"kubernetes": &kubernetesDeploymentAdapter{registry: registry},
	}
}

func buildRuntimeRequestsForProvider(
	ctx context.Context,
	registryService service.RegistryService,
	providerID string,
	excludeDeploymentID string,
) ([]*runtimeregistry.MCPServerRunRequest, []*runtimeregistry.AgentRunRequest, error) {
	provider := strings.TrimSpace(providerID)
	if provider == "" {
		return nil, nil, fmt.Errorf("provider id is required: %w", database.ErrInvalidInput)
	}
	originManaged := "managed"
	filter := &models.DeploymentFilter{
		ProviderID: &provider,
		Origin:     &originManaged,
	}
	deployments, err := registryService.GetDeployments(ctx, filter)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to list managed deployments for provider %s: %w", provider, err)
	}

	serverRequests := make([]*runtimeregistry.MCPServerRunRequest, 0)
	agentRequests := make([]*runtimeregistry.AgentRunRequest, 0)
	for _, deployment := range deployments {
		if deployment == nil || strings.TrimSpace(deployment.ID) == strings.TrimSpace(excludeDeploymentID) {
			continue
		}
		if !isRuntimeManagedDeploymentStatus(deployment.Status) {
			continue
		}
		switch deployment.ResourceType {
		case "mcp":
			serverResp, err := registryService.GetServerByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
			if err != nil {
				if errors.Is(err, database.ErrNotFound) {
					log.Printf("Warning: skipping stale mcp deployment %s (%s@%s): server not found", deployment.ID, deployment.ServerName, deployment.Version)
					continue
				}
				return nil, nil, fmt.Errorf("failed to load mcp server %s@%s: %w", deployment.ServerName, deployment.Version, err)
			}
			envValues, argValues, headerValues := splitDeploymentRuntimeInputs(deployment.Env)
			serverRequests = append(serverRequests, &runtimeregistry.MCPServerRunRequest{
				RegistryServer: &serverResp.Server,
				DeploymentID:   deployment.ID,
				PreferRemote:   deployment.PreferRemote,
				EnvValues:      envValues,
				ArgValues:      argValues,
				HeaderValues:   headerValues,
			})
		case "agent":
			agentResp, err := registryService.GetAgentByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
			if err != nil {
				if errors.Is(err, database.ErrNotFound) {
					log.Printf("Warning: skipping stale agent deployment %s (%s@%s): agent not found", deployment.ID, deployment.ServerName, deployment.Version)
					continue
				}
				return nil, nil, fmt.Errorf("failed to load agent %s@%s: %w", deployment.ServerName, deployment.Version, err)
			}
			envValues := copyStringMap(deployment.Env)
			resolvedMCPServers, err := resolveAgentManifestMCPServers(
				ctx,
				registryService,
				deployment.ID,
				&agentResp.Agent.AgentManifest,
				envValues["KAGENT_NAMESPACE"],
			)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to resolve agent MCP servers for %s@%s: %w", deployment.ServerName, deployment.Version, err)
			}
			agentRequests = append(agentRequests, &runtimeregistry.AgentRunRequest{
				RegistryAgent:      &agentResp.Agent,
				DeploymentID:       deployment.ID,
				EnvValues:          envValues,
				ResolvedMCPServers: resolvedMCPServers,
			})
		}
	}

	return serverRequests, agentRequests, nil
}

func resolveAgentManifestMCPServers(
	ctx context.Context,
	registryService service.RegistryService,
	deploymentID string,
	manifest *models.AgentManifest,
	kagentNamespace string,
) ([]*runtimeregistry.MCPServerRunRequest, error) {
	if manifest == nil {
		return nil, nil
	}
	resolvedServers := make([]*runtimeregistry.MCPServerRunRequest, 0)
	for _, mcpServer := range manifest.McpServers {
		if mcpServer.Type != "registry" {
			continue
		}

		version := mcpServer.RegistryServerVersion
		if version == "" {
			version = "latest"
		}
		serverResp, err := registryService.GetServerByNameAndVersion(ctx, mcpServer.RegistryServerName, version)
		if err != nil {
			return nil, fmt.Errorf("failed to get server %q version %s: %w", mcpServer.RegistryServerName, version, err)
		}
		envValues := map[string]string{}
		if strings.TrimSpace(kagentNamespace) != "" {
			envValues["KAGENT_NAMESPACE"] = strings.TrimSpace(kagentNamespace)
		}
		resolvedServers = append(resolvedServers, &runtimeregistry.MCPServerRunRequest{
			RegistryServer: &serverResp.Server,
			DeploymentID:   deploymentID,
			PreferRemote:   mcpServer.RegistryServerPreferRemote,
			EnvValues:      envValues,
			ArgValues:      map[string]string{},
			HeaderValues:   map[string]string{},
		})
	}
	return resolvedServers, nil
}

func isRuntimeManagedDeploymentStatus(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "", "pending", "active", "deploying", "deployed":
		return true
	default:
		return false
	}
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func splitDeploymentRuntimeInputs(input map[string]string) (map[string]string, map[string]string, map[string]string) {
	if len(input) == 0 {
		return map[string]string{}, map[string]string{}, map[string]string{}
	}
	envValues := make(map[string]string, len(input))
	argValues := map[string]string{}
	headerValues := map[string]string{}
	for key, value := range input {
		switch {
		case strings.HasPrefix(key, "ARG_"):
			name := strings.TrimPrefix(key, "ARG_")
			if name != "" {
				argValues[name] = value
			}
		case strings.HasPrefix(key, "HEADER_"):
			name := strings.TrimPrefix(key, "HEADER_")
			if name != "" {
				headerValues[name] = value
			}
		default:
			envValues[key] = value
		}
	}
	return envValues, argValues, headerValues
}
