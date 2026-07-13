package local

import (
	"context"
	"fmt"
	"maps"
	"strings"

	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
	"github.com/agentregistry-dev/agentregistry/pkg/gateway"
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

	if err := a.engine.Apply(ctx, gateway.Target{Name: deploymentID}, config.GatewayConfig); err != nil {
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
