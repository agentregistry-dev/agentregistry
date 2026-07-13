package local

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

// LocalApplier applies and removes rendered agentgateway config against the
// on-disk agent-gateway.yaml for a local docker-compose runtime. It
// implements agentgateway.Applier structurally, without importing that
// package, keying merge/remove operations by gateway.Target.Name (the
// deployment id).
type LocalApplier struct {
	runtimeDir       string
	agentGatewayPort uint16
}

// NewLocalApplier constructs a LocalApplier pinned to a runtime directory
// (agent-gateway.yaml lives here) and the port the agentgateway service
// binds.
func NewLocalApplier(runtimeDir string, agentGatewayPort uint16) *LocalApplier {
	return &LocalApplier{runtimeDir: runtimeDir, agentGatewayPort: agentGatewayPort}
}

// Apply merges rendered targets/routes into the on-disk agent-gateway.yaml,
// upserting by target/route name. A nil rendered config is a no-op.
func (a *LocalApplier) Apply(_ context.Context, _ gateway.Target, rendered *runtimetypes.AgentGatewayConfig) error {
	if rendered == nil {
		return nil
	}
	existing, err := LoadLocalAgentGatewayConfig(a.runtimeDir, a.agentGatewayPort)
	if err != nil {
		return err
	}
	targetNames := extractTargetNames(rendered)
	routeNames := extractNonMCPRouteNames(rendered)
	mergeAgentGatewayConfig(existing, rendered, targetNames, routeNames, a.agentGatewayPort)
	return writeLocalAgentGatewayConfig(a.runtimeDir, existing, a.agentGatewayPort)
}

// Remove strips every gateway target/route whose name contains the
// deployment id in target.Name. Idempotent: calling it again once nothing
// matches is a no-op.
func (a *LocalApplier) Remove(_ context.Context, target gateway.Target) error {
	deploymentID := strings.TrimSpace(target.Name)
	if deploymentID == "" {
		return fmt.Errorf("local applier: target.Name (deployment id) is required")
	}
	existing, err := LoadLocalAgentGatewayConfig(a.runtimeDir, a.agentGatewayPort)
	if err != nil {
		return err
	}
	filterGatewayRoutesByDeploymentID(existing, deploymentID)
	return writeLocalAgentGatewayConfig(a.runtimeDir, existing, a.agentGatewayPort)
}
