package local

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type localDeploymentAdapter struct {
	registry         service.RegistryService
	platformDir      string
	agentGatewayPort uint16
}

func NewLocalDeploymentAdapter(
	registry service.RegistryService,
	platformDir string,
	agentGatewayPort uint16,
) *localDeploymentAdapter {
	return &localDeploymentAdapter{
		registry:         registry,
		platformDir:      platformDir,
		agentGatewayPort: agentGatewayPort,
	}
}

func (a *localDeploymentAdapter) Platform() string { return "local" }

func (a *localDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *localDeploymentAdapter) Deploy(ctx context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	if err := utils.ValidateDeploymentRequest(req, false); err != nil {
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
	if err := utils.ValidateDeploymentRequest(deployment, true); err != nil {
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
	return nil, utils.ErrDeploymentNotSupported
}

func (a *localDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error {
	return utils.ErrDeploymentNotSupported
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
	cfg, err := BuildLocalPlatformConfig(ctx, a.platformDir, a.agentGatewayPort, "", desired)
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
		server, err := utils.BuildPlatformMCPServer(ctx, a.registry, deployment, "")
		if err != nil {
			return nil, nil, nil, err
		}
		return &platformtypes.DesiredState{MCPServers: []*platformtypes.MCPServer{server}}, nil, nil, nil
	case "agent":
		resolved, err := utils.ResolveAgent(ctx, a.registry, deployment)
		if err != nil {
			return nil, nil, nil, err
		}
		pythonServers := append(common.PythonServersFromManifest(mustAgentManifest(ctx, a.registry, deployment)), resolved.PythonConfigServers...)
		target := &common.MCPConfigTarget{
			BaseDir:   a.platformDir,
			AgentName: resolved.Agent.Name,
			Version:   resolved.Agent.Version,
		}
		return &platformtypes.DesiredState{
			Agents:     []*platformtypes.Agent{resolved.Agent},
			MCPServers: resolved.ResolvedPlatformServers,
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
		return ComposeUpLocalPlatform(ctx, a.platformDir, false)
	}

	composeCfg, err := LoadLocalDockerComposeConfig(a.platformDir)
	if err != nil {
		return err
	}
	gatewayCfg, err := LoadLocalAgentGatewayConfig(a.platformDir, a.agentGatewayPort)
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

	if err := WriteLocalPlatformFiles(a.platformDir, &platformtypes.LocalPlatformConfig{
		DockerCompose: composeCfg,
		AgentGateway:  gatewayCfg,
	}, a.agentGatewayPort); err != nil {
		return err
	}
	return ComposeUpLocalPlatform(ctx, a.platformDir, false)
}
