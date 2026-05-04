package local

import (
	"context"
	"fmt"
	"time"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// localDeploymentAdapter serves Deployments onto a local docker-compose
// runtime. Pinned at construction time to a runtime directory
// (docker-compose.yaml + agent-gateway.yaml live there) and the port the
// agentgateway service binds.
type localDeploymentAdapter struct {
	platformDir      string
	agentGatewayPort uint16
}

// runLocalComposeUp / runLocalComposeDown are package vars rather than
// direct calls so adapter_test.go can stub the docker-compose shell-out
// without spinning up a real compose stack.
var (
	runLocalComposeUp   = ComposeUpLocalPlatform
	runLocalComposeDown = ComposeDownLocalPlatform
)

// NewLocalDeploymentAdapter constructs an adapter pinned to a runtime
// directory (docker-compose.yaml + agent-gateway.yaml live here) and the
// port the agentgateway service binds.
func NewLocalDeploymentAdapter(platformDir string, agentGatewayPort uint16) *localDeploymentAdapter {
	return &localDeploymentAdapter{
		platformDir:      platformDir,
		agentGatewayPort: agentGatewayPort,
	}
}

func (a *localDeploymentAdapter) Platform() string { return "local" }

// SupportedTargetKinds reports the v1alpha1 Kinds this adapter can deploy:
// Agent and MCPServer.
func (a *localDeploymentAdapter) SupportedTargetKinds() []string {
	return []string{
		v1alpha1.KindAgent,
		v1alpha1.KindMCPServer,
		v1alpha1.KindRemoteMCPServer,
	}
}

// Apply materializes the Deployment's target onto the local docker-compose
// runtime. Apply is async in the v1alpha1 contract: the returned
// Progressing condition captures that the compose stack was asked to
// converge; downstream convergence tracking (Ready=True) arrives via the
// reconciler's watch loop (Phase 2 / KRT), not this method.
func (a *localDeploymentAdapter) Apply(ctx context.Context, in types.ApplyInput) (*types.ApplyResult, error) {
	if in.Deployment == nil {
		return nil, fmt.Errorf("apply: deployment is required")
	}
	desired, err := a.buildDesiredStateFromV1Alpha1(ctx, in)
	if err != nil {
		return nil, err
	}
	cfg, err := BuildLocalPlatformConfig(ctx, a.platformDir, a.agentGatewayPort, "", desired)
	if err != nil {
		return nil, fmt.Errorf("build local platform config: %w", err)
	}
	if err := a.mergeAndApplyLocalPlatform(ctx, cfg, false); err != nil {
		return nil, fmt.Errorf("apply local platform: %w", err)
	}

	now := time.Now().UTC()
	gen := in.Deployment.Metadata.Generation
	return &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Progressing",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Applied",
			Message:            "docker-compose stack reconciled; waiting for workload convergence",
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}, {
			Type:               "ProviderConfigured",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "LocalProvider",
			Message:            "local provider ready",
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}},
	}, nil
}

// Remove tears down compose services attributed to this deployment.
// Idempotent: if no services match the deployment name, the gateway
// routes are still scrubbed and the method succeeds. Row lifetime is
// owned by the soft-delete + GC path; the adapter only handles
// external-state teardown.
func (a *localDeploymentAdapter) Remove(ctx context.Context, in types.RemoveInput) (*types.RemoveResult, error) {
	if in.Deployment == nil {
		return nil, fmt.Errorf("remove: deployment is required")
	}
	deploymentID := in.Deployment.Metadata.Name
	if err := a.removeLocalDeploymentArtifactsByID(ctx, deploymentID); err != nil {
		return nil, fmt.Errorf("remove local platform artifacts: %w", err)
	}

	now := time.Now().UTC()
	gen := in.Deployment.Metadata.Generation
	return &types.RemoveResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "Removed",
			Message:            "docker-compose stack torn down",
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}},
	}, nil
}

// Logs is not yet implemented for the local adapter. Returns an
// immediately-closed channel so callers don't block.
func (a *localDeploymentAdapter) Logs(ctx context.Context, in types.LogsInput) (<-chan types.LogLine, error) {
	ch := make(chan types.LogLine)
	close(ch)
	return ch, nil
}

// Discover reports no out-of-band local deployments — out-of-band
// workloads only make sense for remote/hosted platforms.
func (a *localDeploymentAdapter) Discover(ctx context.Context, in types.DiscoverInput) ([]types.DiscoveryResult, error) {
	return nil, nil
}

// buildDesiredStateFromV1Alpha1 constructs a *platformtypes.DesiredState from
// the v1alpha1 ApplyInput. The target dispatches by Kind:
//   - MCPServer → one-shot translate; no ref walk.
//   - Agent     → translate spec + resolve every MCPServers entry through
//     in.Getter to build the gateway's upstream map.
func (a *localDeploymentAdapter) buildDesiredStateFromV1Alpha1(
	ctx context.Context,
	in types.ApplyInput,
) (*platformtypes.DesiredState, error) {
	if in.Target == nil {
		return nil, fmt.Errorf("apply: target is required")
	}
	deploymentID := in.Deployment.Metadata.Name
	envValues, argValues, headerValues := utils.SplitDeploymentRuntimeInputs(in.Deployment.Spec.Env)

	switch target := in.Target.(type) {
	case *v1alpha1.MCPServer:
		server, err := utils.SpecToPlatformMCPServer(ctx, target.Metadata, target.Spec, utils.MCPServerTranslateOpts{
			DeploymentID: deploymentID,
			EnvValues:    envValues,
			ArgValues:    argValues,
		})
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{MCPServers: []*platformtypes.MCPServer{server}}, nil
	case *v1alpha1.RemoteMCPServer:
		server, err := utils.SpecToPlatformRemoteMCPServer(ctx, target.Metadata, target.Spec, utils.RemoteMCPServerTranslateOpts{
			DeploymentID: deploymentID,
			HeaderValues: headerValues,
		})
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{MCPServers: []*platformtypes.MCPServer{server}}, nil
	case *v1alpha1.Agent:
		var telemetryEndpoint string
		if in.Provider != nil {
			telemetryEndpoint = in.Provider.Spec.TelemetryEndpoint
		}
		agent, servers, err := utils.SpecToPlatformAgent(ctx, target.Metadata, target.Spec, utils.AgentTranslateOpts{
			DeploymentID:      deploymentID,
			KagentURL:         "http://localhost",
			DeploymentEnv:     envValues,
			TelemetryEndpoint: telemetryEndpoint,
			HeaderValues:      headerValues,
			Getter:            in.Getter,
		})
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{
			Agents:     []*platformtypes.Agent{agent},
			MCPServers: servers,
		}, nil
	default:
		return nil, fmt.Errorf("apply: unsupported target kind %q", in.Target.GetKind())
	}
}

// Compile-time assertion that the local adapter satisfies the v1alpha1
// DeploymentAdapter contract.
var _ types.DeploymentAdapter = (*localDeploymentAdapter)(nil)
