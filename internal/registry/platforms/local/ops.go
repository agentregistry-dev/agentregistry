package local

import (
	"context"
	"fmt"
	"maps"
	"strings"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// mergeAndApplyLocalPlatform loads the current docker-compose +
// agent-gateway on-disk state, overlays (or strips, when remove=true) the
// services + gateway routes produced by BuildLocalPlatformConfig, writes
// the merged files back, and runs docker compose up/down accordingly.
//
// Shared between the v1alpha1 Apply path and any future incremental
// reconciler — no ties to the v1alpha1 envelope type.
func (a *localDeploymentAdapter) mergeAndApplyLocalPlatform(
	ctx context.Context,
	config *platformtypes.LocalPlatformConfig,
	remove bool,
) error {
	if config == nil {
		return runLocalComposeUp(ctx, a.platformDir, false)
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
		maps.Copy(composeCfg.Services, config.DockerCompose.Services)
	}

	mergeAgentGatewayConfig(gatewayCfg, config.AgentGateway, targetNames, routeNames, remove, a.agentGatewayPort)

	if err := WriteLocalPlatformFiles(a.platformDir, &platformtypes.LocalPlatformConfig{
		DockerCompose: composeCfg,
		AgentGateway:  gatewayCfg,
	}, a.agentGatewayPort); err != nil {
		return err
	}
	if len(composeCfg.Services) == 0 {
		return runLocalComposeDown(ctx, a.platformDir, false)
	}
	return runLocalComposeUp(ctx, a.platformDir, false)
}

// removeLocalDeploymentArtifactsByID strips every compose service + gateway
// route whose name contains the deployment's id, then writes back and
// converges docker compose. Safe to call repeatedly — no-op once the
// deployment's artifacts are gone.
func (a *localDeploymentAdapter) removeLocalDeploymentArtifactsByID(ctx context.Context, deploymentID string) error {
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}

	composeCfg, err := LoadLocalDockerComposeConfig(a.platformDir)
	if err != nil {
		return err
	}
	gatewayCfg, err := LoadLocalAgentGatewayConfig(a.platformDir, a.agentGatewayPort)
	if err != nil {
		return err
	}

	for serviceName := range composeCfg.Services {
		if strings.Contains(serviceName, deploymentID) {
			delete(composeCfg.Services, serviceName)
		}
	}

	filterGatewayRoutesByDeploymentID(gatewayCfg, deploymentID)

	if err := WriteLocalPlatformFiles(a.platformDir, &platformtypes.LocalPlatformConfig{
		DockerCompose: composeCfg,
		AgentGateway:  gatewayCfg,
	}, a.agentGatewayPort); err != nil {
		return err
	}
	if len(composeCfg.Services) == 0 {
		return runLocalComposeDown(ctx, a.platformDir, false)
	}
	return runLocalComposeUp(ctx, a.platformDir, false)
}

func filterGatewayRoutesByDeploymentID(gatewayCfg *platformtypes.AgentGatewayConfig, deploymentID string) {
	listener := localAgentGatewayListener(gatewayCfg)
	if listener == nil {
		return
	}

	filteredRoutes := make([]platformtypes.LocalRoute, 0, len(listener.Routes))
	for _, route := range listener.Routes {
		filteredRoute, keep := filterGatewayRouteByDeploymentID(route, deploymentID)
		if keep {
			filteredRoutes = append(filteredRoutes, filteredRoute)
		}
	}
	listener.Routes = filteredRoutes
}

func localAgentGatewayListener(gatewayCfg *platformtypes.AgentGatewayConfig) *platformtypes.LocalListener {
	if gatewayCfg == nil || len(gatewayCfg.Binds) == 0 || len(gatewayCfg.Binds[0].Listeners) == 0 {
		return nil
	}
	return &gatewayCfg.Binds[0].Listeners[0]
}

func filterGatewayRouteByDeploymentID(route platformtypes.LocalRoute, deploymentID string) (platformtypes.LocalRoute, bool) {
	if route.RouteName == localMCPRouteName {
		return filterMCPGatewayRouteTargets(route, deploymentID)
	}
	return route, !strings.Contains(route.RouteName, deploymentID)
}

func filterMCPGatewayRouteTargets(route platformtypes.LocalRoute, deploymentID string) (platformtypes.LocalRoute, bool) {
	if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
		return route, false
	}

	filteredTargets := make([]platformtypes.MCPTarget, 0, len(route.Backends[0].MCP.Targets))
	for _, target := range route.Backends[0].MCP.Targets {
		if strings.Contains(target.Name, deploymentID) {
			continue
		}
		filteredTargets = append(filteredTargets, target)
	}
	route.Backends[0].MCP.Targets = filteredTargets
	return route, len(filteredTargets) > 0
}
