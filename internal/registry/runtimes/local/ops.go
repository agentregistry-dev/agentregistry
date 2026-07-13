package local

import (
	"context"
	"fmt"
	"maps"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// mergeAndApplyLocalRuntime loads the current docker-compose on-disk state,
// overlays the services produced by BuildLocalRuntimeConfig, writes the
// merged compose file back, delegates the gateway-config half to the
// adapter's engine keyed by deploymentID, and runs docker compose up/down
// accordingly.
//
// Shared between the v1alpha1 Apply path and any future incremental
// reconciler — no ties to the v1alpha1 envelope type.
func (a *localDeploymentAdapter) mergeAndApplyLocalRuntime(
	ctx context.Context,
	deploymentID string,
	config *runtimetypes.LocalRuntimeConfig,
) error {
	if config == nil {
		return runLocalComposeUp(ctx, a.runtimeDir, false)
	}

	composeCfg, err := LoadLocalDockerComposeConfig(a.runtimeDir)
	if err != nil {
		return err
	}

	serviceNames := extractServiceNames(config)
	for _, name := range serviceNames {
		delete(composeCfg.Services, name)
	}
	maps.Copy(composeCfg.Services, config.DockerCompose.Services)

	if err := a.engine.Apply(ctx, gateway.Target{Name: deploymentID}, config.AgentGateway); err != nil {
		return err
	}
	if err := writeLocalDockerComposeConfig(a.runtimeDir, composeCfg); err != nil {
		return err
	}
	if len(composeCfg.Services) == 0 {
		return runLocalComposeDown(ctx, a.runtimeDir, false)
	}
	return runLocalComposeUp(ctx, a.runtimeDir, false)
}

// removeLocalDeploymentArtifactsByID strips every compose service whose name
// contains the deployment's id, delegates gateway route removal to the
// adapter's engine, writes back, and converges docker compose. Safe to call
// repeatedly — no-op once the deployment's artifacts are gone.
func (a *localDeploymentAdapter) removeLocalDeploymentArtifactsByID(ctx context.Context, deploymentID string) error {
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
	}

	composeCfg, err := LoadLocalDockerComposeConfig(a.runtimeDir)
	if err != nil {
		return err
	}

	for serviceName := range composeCfg.Services {
		if strings.Contains(serviceName, deploymentID) {
			delete(composeCfg.Services, serviceName)
		}
	}

	if err := a.engine.Remove(ctx, gateway.Target{Name: deploymentID}); err != nil {
		return err
	}
	if err := writeLocalDockerComposeConfig(a.runtimeDir, composeCfg); err != nil {
		return err
	}
	if len(composeCfg.Services) == 0 {
		return runLocalComposeDown(ctx, a.runtimeDir, false)
	}
	return runLocalComposeUp(ctx, a.runtimeDir, false)
}

func filterGatewayRoutesByDeploymentID(gatewayCfg *runtimetypes.AgentGatewayConfig, deploymentID string) {
	listener := localAgentGatewayListener(gatewayCfg)
	if listener == nil {
		return
	}

	filteredRoutes := make([]runtimetypes.LocalRoute, 0, len(listener.Routes))
	for _, route := range listener.Routes {
		filteredRoute, keep := filterGatewayRouteByDeploymentID(route, deploymentID)
		if keep {
			filteredRoutes = append(filteredRoutes, filteredRoute)
		}
	}
	listener.Routes = filteredRoutes
}

func localAgentGatewayListener(gatewayCfg *runtimetypes.AgentGatewayConfig) *runtimetypes.LocalListener {
	if gatewayCfg == nil || len(gatewayCfg.Binds) == 0 || len(gatewayCfg.Binds[0].Listeners) == 0 {
		return nil
	}
	return &gatewayCfg.Binds[0].Listeners[0]
}

func filterGatewayRouteByDeploymentID(route runtimetypes.LocalRoute, deploymentID string) (runtimetypes.LocalRoute, bool) {
	if route.RouteName == localMCPRouteName {
		return filterMCPGatewayRouteTargets(route, deploymentID)
	}
	return route, !strings.Contains(route.RouteName, deploymentID)
}

func filterMCPGatewayRouteTargets(route runtimetypes.LocalRoute, deploymentID string) (runtimetypes.LocalRoute, bool) {
	if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
		return route, false
	}

	filteredTargets := make([]runtimetypes.MCPTarget, 0, len(route.Backends[0].MCP.Targets))
	for _, target := range route.Backends[0].MCP.Targets {
		if strings.Contains(target.Name, deploymentID) {
			continue
		}
		filteredTargets = append(filteredTargets, target)
	}
	route.Backends[0].MCP.Targets = filteredTargets
	return route, len(filteredTargets) > 0
}
