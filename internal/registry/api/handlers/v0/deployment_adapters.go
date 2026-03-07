package v0

import (
	"context"
	"errors"
	"fmt"
	"log"
	"maps"
	"sort"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	kubernetesplatform "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/kubernetes"
	localplatform "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/local"
	platformshared "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/shared"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

var errDeploymentNotSupported = errors.New("deployment operation is not supported for this provider platform type")

const (
	defaultKubernetesProviderID = "kubernetes-default"
	managedLabelKey             = "aregistry.ai/managed"
	managedLabelValue           = "true"
	localMCPRouteName           = "mcp_route"
)

type localDeploymentAdapter struct {
	registry         service.RegistryService
	platformDir      string
	agentGatewayPort uint16
}

type kubernetesDeploymentAdapter struct {
	registry service.RegistryService
}

type agentPlatformMaterialization struct {
	agent                   *platformtypes.Agent
	resolvedPlatformServers []*platformtypes.MCPServer
	resolvedConfigServers   []platformtypes.ResolvedMCPServerConfig
	pythonConfigServers     []common.PythonMCPServer
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
	if err := validateAdapterDeploymentRequest(req, false); err != nil {
		return nil, err
	}

	translated, pythonServers, agentTarget, err := a.translateLocalDeployment(ctx, req)
	if err != nil {
		return nil, err
	}

	if err := a.mergeAndApplyLocalPlatform(ctx, translated, false); err != nil {
		return nil, err
	}

	if agentTarget != nil {
		if err := common.RefreshMCPConfig(agentTarget, pythonServers, false); err != nil {
			return nil, fmt.Errorf("refresh agent MCP config: %w", err)
		}
	}

	return &models.DeploymentActionResult{Status: "deployed"}, nil
}

func (a *localDeploymentAdapter) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if err := validateAdapterDeploymentRequest(deployment, true); err != nil {
		return err
	}

	translated, _, agentTarget, err := a.translateLocalDeployment(ctx, deployment)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return err
	}

	if err := a.mergeAndApplyLocalPlatform(ctx, translated, true); err != nil {
		return err
	}

	if agentTarget != nil {
		if err := common.RefreshMCPConfig(agentTarget, nil, false); err != nil {
			return fmt.Errorf("cleanup agent MCP config: %w", err)
		}
	}
	return nil
}

func (a *localDeploymentAdapter) CleanupStale(_ context.Context, _ *models.Deployment) error {
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

func (a *localDeploymentAdapter) translateLocalDeployment(
	ctx context.Context,
	deployment *models.Deployment,
) (*platformtypes.LocalPlatformConfig, []common.PythonMCPServer, *common.MCPConfigTarget, error) {
	if deployment == nil {
		return nil, nil, nil, nil
	}
	desired, pythonServers, agentTarget, err := a.buildLocalDesiredState(ctx, deployment)
	if err != nil {
		return nil, nil, nil, err
	}
	translator := localplatform.NewAgentGatewayTranslator(a.platformDir, a.agentGatewayPort)
	cfg, err := translator.TranslatePlatformConfig(ctx, desired)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("translate local platform config: %w", err)
	}
	if cfg == nil {
		return nil, nil, nil, fmt.Errorf("local platform config is required")
	}
	return cfg, pythonServers, agentTarget, nil
}

func (a *localDeploymentAdapter) buildLocalDesiredState(
	ctx context.Context,
	deployment *models.Deployment,
) (*platformtypes.DesiredState, []common.PythonMCPServer, *common.MCPConfigTarget, error) {
	resourceType := strings.ToLower(strings.TrimSpace(deployment.ResourceType))
	switch resourceType {
	case "mcp":
		server, err := buildPlatformMCPServer(ctx, a.registry, deployment, "")
		if err != nil {
			return nil, nil, nil, err
		}
		return &platformtypes.DesiredState{MCPServers: []*platformtypes.MCPServer{server}}, nil, nil, nil
	case "agent":
		materialized, err := buildLocalAgentMaterialization(ctx, a.registry, deployment)
		if err != nil {
			return nil, nil, nil, err
		}
		pythonServers := append(common.PythonServersFromManifest(mustAgentManifest(ctx, a.registry, deployment)), materialized.pythonConfigServers...)
		target := &common.MCPConfigTarget{
			BaseDir:   a.platformDir,
			AgentName: materialized.agent.Name,
			Version:   materialized.agent.Version,
		}
		return &platformtypes.DesiredState{
			Agents:     []*platformtypes.Agent{materialized.agent},
			MCPServers: materialized.resolvedPlatformServers,
		}, pythonServers, target, nil
	default:
		return nil, nil, nil, fmt.Errorf("invalid resource type %q: %w", deployment.ResourceType, database.ErrInvalidInput)
	}
}

func (a *localDeploymentAdapter) mergeAndApplyLocalPlatform(
	ctx context.Context,
	config *platformtypes.LocalPlatformConfig,
	remove bool,
) error {
	if config == nil {
		return localplatform.ComposeUp(ctx, a.platformDir, false)
	}

	composeCfg, err := localplatform.LoadDockerComposeConfig(a.platformDir)
	if err != nil {
		return err
	}
	gatewayCfg, err := localplatform.LoadAgentGatewayConfig(a.platformDir, a.agentGatewayPort)
	if err != nil {
		return err
	}

	serviceNames := extractServiceNames(config)
	targetNames := extractTargetNames(config.AgentGateway)
	routeNames := extractNonMCPRouteNames(config.AgentGateway)

	for _, name := range serviceNames {
		delete(composeCfg.Services, name)
	}
	if !remove {
		for name, serviceCfg := range config.DockerCompose.Services {
			composeCfg.Services[name] = serviceCfg
		}
	}

	mergeAgentGatewayConfig(gatewayCfg, config.AgentGateway, targetNames, routeNames, remove, a.agentGatewayPort)

	if err := localplatform.WriteDockerComposeConfig(a.platformDir, composeCfg); err != nil {
		return err
	}
	if err := localplatform.WriteAgentGatewayConfig(a.platformDir, gatewayCfg, a.agentGatewayPort); err != nil {
		return err
	}
	return localplatform.ComposeUp(ctx, a.platformDir, false)
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
			platformDir:      settings.RuntimeDir,
			agentGatewayPort: settings.AgentGatewayPort,
		},
		"kubernetes": &kubernetesDeploymentAdapter{registry: registry},
	}
}

func validateAdapterDeploymentRequest(deployment *models.Deployment, allowExisting bool) error {
	if deployment == nil {
		return fmt.Errorf("deployment request is required: %w", database.ErrInvalidInput)
	}
	if strings.TrimSpace(deployment.ProviderID) == "" {
		return fmt.Errorf("provider id is required: %w", database.ErrInvalidInput)
	}
	if len(deployment.ProviderConfig) > 0 {
		return fmt.Errorf("providerConfig is not supported for OSS adapters: %w", database.ErrInvalidInput)
	}
	if allowExisting {
		if strings.TrimSpace(deployment.ID) == "" {
			return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
		}
	}
	return nil
}

func buildLocalAgentMaterialization(
	ctx context.Context,
	registryService service.RegistryService,
	deployment *models.Deployment,
) (*agentPlatformMaterialization, error) {
	agentResp, err := registryService.GetAgentByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
	if err != nil {
		return nil, fmt.Errorf("load agent %s@%s: %w", deployment.ServerName, deployment.Version, err)
	}
	envValues := copyStringMap(deployment.Env)
	envValues["KAGENT_URL"] = "http://localhost"
	envValues["KAGENT_NAME"] = agentResp.Agent.AgentManifest.Name
	envValues["AGENT_NAME"] = agentResp.Agent.AgentManifest.Name
	envValues["MODEL_PROVIDER"] = agentResp.Agent.AgentManifest.ModelProvider
	envValues["MODEL_NAME"] = agentResp.Agent.AgentManifest.ModelName

	port, err := utils.FindAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("find local agent port: %w", err)
	}

	agent := &platformtypes.Agent{
		Name:         agentResp.Agent.Name,
		Version:      agentResp.Agent.Version,
		DeploymentID: deployment.ID,
		Deployment: platformtypes.AgentDeployment{
			Image: agentResp.Agent.Image,
			Env:   envValues,
			Port:  port,
		},
	}

	resolvedServers, resolvedConfigs, pythonServers, err := resolveAgentManifestPlatformMCPServers(ctx, registryService, deployment.ID, &agentResp.Agent.AgentManifest, "")
	if err != nil {
		return nil, err
	}

	return &agentPlatformMaterialization{
		agent:                   agent,
		resolvedPlatformServers: resolvedServers,
		resolvedConfigServers:   resolvedConfigs,
		pythonConfigServers:     pythonServers,
	}, nil
}

func buildKubernetesAgentMaterialization(
	ctx context.Context,
	registryService service.RegistryService,
	deployment *models.Deployment,
	namespace string,
) (*agentPlatformMaterialization, error) {
	agentResp, err := registryService.GetAgentByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
	if err != nil {
		return nil, fmt.Errorf("load agent %s@%s: %w", deployment.ServerName, deployment.Version, err)
	}
	envValues := copyStringMap(deployment.Env)
	if envValues["KAGENT_NAMESPACE"] == "" {
		envValues["KAGENT_NAMESPACE"] = namespace
	}
	envValues["KAGENT_URL"] = "http://localhost"
	envValues["KAGENT_NAME"] = agentResp.Agent.AgentManifest.Name
	envValues["AGENT_NAME"] = agentResp.Agent.AgentManifest.Name
	envValues["MODEL_PROVIDER"] = agentResp.Agent.AgentManifest.ModelProvider
	envValues["MODEL_NAME"] = agentResp.Agent.AgentManifest.ModelName

	resolvedServers, resolvedConfigs, _, err := resolveAgentManifestPlatformMCPServers(ctx, registryService, deployment.ID, &agentResp.Agent.AgentManifest, namespace)
	if err != nil {
		return nil, err
	}
	skills, err := resolveAgentManifestSkills(ctx, registryService, &agentResp.Agent.AgentManifest)
	if err != nil {
		return nil, err
	}

	return &agentPlatformMaterialization{
		agent: &platformtypes.Agent{
			Name:               agentResp.Agent.Name,
			Version:            agentResp.Agent.Version,
			DeploymentID:       deployment.ID,
			Deployment:         platformtypes.AgentDeployment{Image: agentResp.Agent.Image, Env: envValues},
			ResolvedMCPServers: resolvedConfigs,
			Skills:             skills,
		},
		resolvedPlatformServers: resolvedServers,
		resolvedConfigServers:   resolvedConfigs,
	}, nil
}

func buildPlatformMCPServer(
	ctx context.Context,
	registryService service.RegistryService,
	deployment *models.Deployment,
	namespace string,
) (*platformtypes.MCPServer, error) {
	serverResp, err := registryService.GetServerByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
	if err != nil {
		return nil, fmt.Errorf("load mcp server %s@%s: %w", deployment.ServerName, deployment.Version, err)
	}
	envValues, argValues, headerValues := splitDeploymentRuntimeInputs(deployment.Env)
	translator := platformshared.NewTranslator()
	server, err := translator.TranslateMCPServer(ctx, &platformshared.MCPServerRunRequest{
		RegistryServer: &serverResp.Server,
		DeploymentID:   deployment.ID,
		PreferRemote:   deployment.PreferRemote,
		EnvValues:      envValues,
		ArgValues:      argValues,
		HeaderValues:   headerValues,
	})
	if err != nil {
		return nil, err
	}
	if namespace != "" && server.Namespace == "" {
		server.Namespace = namespace
	}
	return server, nil
}

func resolveAgentManifestPlatformMCPServers(
	ctx context.Context,
	registryService service.RegistryService,
	deploymentID string,
	manifest *models.AgentManifest,
	namespace string,
) ([]*platformtypes.MCPServer, []platformtypes.ResolvedMCPServerConfig, []common.PythonMCPServer, error) {
	if manifest == nil {
		return nil, nil, nil, nil
	}

	var platformServers []*platformtypes.MCPServer
	var configServers []platformtypes.ResolvedMCPServerConfig
	var pythonServers []common.PythonMCPServer

	for _, mcpServer := range manifest.McpServers {
		if mcpServer.Type != "registry" {
			continue
		}

		version := strings.TrimSpace(mcpServer.RegistryServerVersion)
		if version == "" {
			version = "latest"
		}

		serverResp, err := registryService.GetServerByNameAndVersion(ctx, mcpServer.RegistryServerName, version)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load resolved MCP server %s@%s: %w", mcpServer.RegistryServerName, version, err)
		}

		translator := platformshared.NewTranslator()
		platformServer, err := translator.TranslateMCPServer(ctx, &platformshared.MCPServerRunRequest{
			RegistryServer: &serverResp.Server,
			DeploymentID:   deploymentID,
			PreferRemote:   mcpServer.RegistryServerPreferRemote,
			EnvValues:      map[string]string{},
			ArgValues:      map[string]string{},
			HeaderValues:   map[string]string{},
		})
		if err != nil {
			return nil, nil, nil, err
		}
		if namespace != "" && platformServer.Namespace == "" {
			platformServer.Namespace = namespace
		}
		platformServers = append(platformServers, platformServer)

		configServer := resolvedMCPConfigFromRegistryServer(&serverResp.Server, deploymentID, mcpServer.RegistryServerPreferRemote)
		configServers = append(configServers, configServer)
		pythonServers = append(pythonServers, common.PythonMCPServer{
			Name:    configServer.Name,
			Type:    configServer.Type,
			URL:     configServer.URL,
			Headers: configServer.Headers,
		})
	}

	return platformServers, configServers, pythonServers, nil
}

func resolvedMCPConfigFromRegistryServer(
	server *apiv0.ServerJSON,
	deploymentID string,
	preferRemote bool,
) platformtypes.ResolvedMCPServerConfig {
	if server == nil {
		return platformtypes.ResolvedMCPServerConfig{
			Name: platformshared.GenerateInternalNameForDeployment("", deploymentID),
			Type: "command",
		}
	}
	cfg := platformtypes.ResolvedMCPServerConfig{
		Name: platformshared.GenerateInternalNameForDeployment(server.Name, deploymentID),
		Type: "command",
	}
	useRemote := len(server.Remotes) > 0 && (preferRemote || len(server.Packages) == 0)
	if !useRemote {
		return cfg
	}
	cfg.Type = "remote"
	cfg.URL = server.Remotes[0].URL
	if len(server.Remotes[0].Headers) > 0 {
		headers := make(map[string]string, len(server.Remotes[0].Headers))
		for _, header := range server.Remotes[0].Headers {
			headers[header.Name] = header.Value
		}
		cfg.Headers = headers
	}
	return cfg
}

func resolveAgentManifestSkills(
	ctx context.Context,
	registryService service.RegistryService,
	manifest *models.AgentManifest,
) ([]platformtypes.AgentSkillRef, error) {
	if manifest == nil || len(manifest.Skills) == 0 {
		return nil, nil
	}
	var resolved []platformtypes.AgentSkillRef
	for _, skill := range manifest.Skills {
		ref, err := resolveSkillRef(ctx, registryService, skill)
		if err != nil {
			return nil, fmt.Errorf("resolve skill %q: %w", skill.Name, err)
		}
		resolved = append(resolved, ref)
	}
	return resolved, nil
}

func resolveSkillRef(
	ctx context.Context,
	registryService service.RegistryService,
	skill models.SkillRef,
) (platformtypes.AgentSkillRef, error) {
	image := strings.TrimSpace(skill.Image)
	registrySkillName := strings.TrimSpace(skill.RegistrySkillName)
	hasImage := image != ""
	hasRegistry := registrySkillName != ""

	if !hasImage && !hasRegistry {
		return platformtypes.AgentSkillRef{}, fmt.Errorf("one of image or registrySkillName is required")
	}
	if hasImage && hasRegistry {
		return platformtypes.AgentSkillRef{}, fmt.Errorf("only one of image or registrySkillName may be set")
	}
	if hasImage {
		return platformtypes.AgentSkillRef{Name: skill.Name, Image: image}, nil
	}

	version := strings.TrimSpace(skill.RegistrySkillVersion)
	if version == "" {
		version = "latest"
	}
	skillResp, err := registryService.GetSkillByNameAndVersion(ctx, registrySkillName, version)
	if err != nil {
		return platformtypes.AgentSkillRef{}, fmt.Errorf("fetch skill %q version %q: %w", registrySkillName, version, err)
	}
	for _, pkg := range skillResp.Skill.Packages {
		typ := strings.ToLower(strings.TrimSpace(pkg.RegistryType))
		if (typ == "docker" || typ == "oci") && strings.TrimSpace(pkg.Identifier) != "" {
			return platformtypes.AgentSkillRef{Name: skill.Name, Image: strings.TrimSpace(pkg.Identifier)}, nil
		}
	}
	if skillResp.Skill.Repository != nil &&
		strings.EqualFold(skillResp.Skill.Repository.Source, "github") &&
		strings.TrimSpace(skillResp.Skill.Repository.URL) != "" {
		return platformtypes.AgentSkillRef{
			Name:    skill.Name,
			RepoURL: strings.TrimSpace(skillResp.Skill.Repository.URL),
		}, nil
	}
	return platformtypes.AgentSkillRef{}, fmt.Errorf("skill %q (version %s): no docker/oci package or github repository found", registrySkillName, version)
}

func mergeAgentGatewayConfig(
	existing *platformtypes.AgentGatewayConfig,
	incoming *platformtypes.AgentGatewayConfig,
	targetNames []string,
	routeNames []string,
	remove bool,
	port uint16,
) {
	localplatform.EnsureAgentGatewayDefaults(existing, port)
	if incoming == nil || len(existing.Binds) == 0 || len(existing.Binds[0].Listeners) == 0 {
		return
	}

	listener := &existing.Binds[0].Listeners[0]
	listener.Routes = filterRoutes(listener.Routes, routeNames)

	targetSet := make(map[string]struct{}, len(targetNames))
	for _, name := range targetNames {
		targetSet[name] = struct{}{}
	}

	var existingTargets []platformtypes.MCPTarget
	var otherRoutes []platformtypes.LocalRoute
	for _, route := range listener.Routes {
		if route.RouteName == localMCPRouteName {
			if len(route.Backends) > 0 && route.Backends[0].MCP != nil {
				for _, target := range route.Backends[0].MCP.Targets {
					if _, shouldRemove := targetSet[target.Name]; !shouldRemove {
						existingTargets = append(existingTargets, target)
					}
				}
			}
			continue
		}
		otherRoutes = append(otherRoutes, route)
	}

	if !remove && incoming != nil {
		existingTargets = append(existingTargets, extractMCPRouteTargets(incoming)...)
		otherRoutes = append(otherRoutes, extractNonMCPRoutes(incoming)...)
	}

	sort.Slice(existingTargets, func(i, j int) bool {
		return existingTargets[i].Name < existingTargets[j].Name
	})
	sort.Slice(otherRoutes, func(i, j int) bool {
		return otherRoutes[i].RouteName < otherRoutes[j].RouteName
	})

	routes := make([]platformtypes.LocalRoute, 0, len(otherRoutes)+1)
	if len(existingTargets) > 0 {
		routes = append(routes, platformtypes.LocalRoute{
			RouteName: localMCPRouteName,
			Matches: []platformtypes.RouteMatch{{
				Path: platformtypes.PathMatch{PathPrefix: "/mcp"},
			}},
			Backends: []platformtypes.RouteBackend{{
				Weight: 100,
				MCP:    &platformtypes.MCPBackend{Targets: existingTargets},
			}},
		})
	}
	routes = append(routes, otherRoutes...)
	listener.Routes = routes
}

func filterRoutes(routes []platformtypes.LocalRoute, names []string) []platformtypes.LocalRoute {
	if len(names) == 0 {
		return routes
	}
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}
	filtered := make([]platformtypes.LocalRoute, 0, len(routes))
	for _, route := range routes {
		if _, remove := nameSet[route.RouteName]; remove {
			continue
		}
		filtered = append(filtered, route)
	}
	return filtered
}

func extractServiceNames(config *platformtypes.LocalPlatformConfig) []string {
	if config == nil || config.DockerCompose == nil {
		return nil
	}
	names := make([]string, 0, len(config.DockerCompose.Services))
	for name := range config.DockerCompose.Services {
		if name == "agent_gateway" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func extractTargetNames(config *platformtypes.AgentGatewayConfig) []string {
	targets := extractMCPRouteTargets(config)
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	sort.Strings(names)
	return names
}

func extractMCPRouteTargets(config *platformtypes.AgentGatewayConfig) []platformtypes.MCPTarget {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName != localMCPRouteName {
			continue
		}
		if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
			return nil
		}
		return append([]platformtypes.MCPTarget{}, route.Backends[0].MCP.Targets...)
	}
	return nil
}

func extractNonMCPRouteNames(config *platformtypes.AgentGatewayConfig) []string {
	routes := extractNonMCPRoutes(config)
	names := make([]string, 0, len(routes))
	for _, route := range routes {
		names = append(names, route.RouteName)
	}
	sort.Strings(names)
	return names
}

func extractNonMCPRoutes(config *platformtypes.AgentGatewayConfig) []platformtypes.LocalRoute {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	var routes []platformtypes.LocalRoute
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName == localMCPRouteName {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func deploymentNamespace(deployment *models.Deployment) string {
	if deployment != nil && deployment.Env != nil {
		if namespace := strings.TrimSpace(deployment.Env["KAGENT_NAMESPACE"]); namespace != "" {
			return namespace
		}
	}
	return kubernetesplatform.DefaultNamespace()
}

func mustAgentManifest(
	ctx context.Context,
	registryService service.RegistryService,
	deployment *models.Deployment,
) *models.AgentManifest {
	agentResp, err := registryService.GetAgentByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
	if err != nil {
		return nil
	}
	manifestCopy := agentResp.Agent.AgentManifest
	return &manifestCopy
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	maps.Copy(out, input)
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
