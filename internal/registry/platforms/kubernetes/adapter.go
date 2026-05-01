package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/constants"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// kubernetesDeploymentAdapter serves Deployments onto a kagent-equipped
// Kubernetes cluster. Stateless — each Apply/Remove/Discover builds a
// fresh controller-runtime client from the supplied v1alpha1.Provider's
// Spec.Config map.
type kubernetesDeploymentAdapter struct{}

// NewKubernetesDeploymentAdapter constructs an adapter that resolves
// every per-call target cluster from the supplied v1alpha1.Provider's
// Spec.Config map.
func NewKubernetesDeploymentAdapter() *kubernetesDeploymentAdapter {
	return &kubernetesDeploymentAdapter{}
}

func (a *kubernetesDeploymentAdapter) Platform() string { return "kubernetes" }

// SupportedTargetKinds reports the v1alpha1 Kinds this adapter can
// deploy: Agent and MCPServer.
func (a *kubernetesDeploymentAdapter) SupportedTargetKinds() []string {
	return []string{
		v1alpha1.KindAgent,
		v1alpha1.KindMCPServer,
		v1alpha1.KindRemoteMCPServer,
	}
}

// Apply translates + applies kagent/kmcp CRDs onto the provider's cluster.
// Returns Progressing=True immediately; the reconciler's (Phase 2 KRT) watch
// loop is responsible for flipping Ready=True once the rollout converges.
// Adapters MAY produce a Degraded condition on permanent translation or
// apply errors; transient failures bubble up as a returned error.
func (a *kubernetesDeploymentAdapter) Apply(ctx context.Context, in types.ApplyInput) (*types.ApplyResult, error) {
	if in.Deployment == nil {
		return nil, fmt.Errorf("apply: deployment is required")
	}
	namespace := namespaceFromV1Alpha1(in.Deployment, in.Provider)

	desired, err := a.buildDesiredStateFromV1Alpha1(ctx, in, namespace)
	if err != nil {
		return nil, err
	}
	cfg, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("translate kubernetes platform config: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("kubernetes platform config is required")
	}
	if err := kubernetesApplyPlatformConfig(ctx, in.Provider, cfg, false); err != nil {
		return nil, fmt.Errorf("apply kubernetes platform config: %w", err)
	}

	now := time.Now().UTC()
	gen := in.Deployment.Metadata.Generation
	return &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Progressing",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Applied",
			Message:            "kagent resources reconciled; waiting for rollout",
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}, {
			Type:               "ProviderConfigured",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "KubernetesProvider",
			Message:            "kubernetes provider reachable",
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}},
	}, nil
}

// Remove deletes every kagent/kmcp resource owned by this Deployment (agent
// + mcp + remote-mcp kinds) via the shared deploymentID label selector. Both
// target kinds are swept because RemoveInput doesn't carry the resolved
// target; sweep-both is cheap and idempotent.
func (a *kubernetesDeploymentAdapter) Remove(ctx context.Context, in types.RemoveInput) (*types.RemoveResult, error) {
	if in.Deployment == nil {
		return nil, fmt.Errorf("remove: deployment is required")
	}
	namespace := namespaceFromV1Alpha1(in.Deployment, in.Provider)
	deploymentID := in.Deployment.Metadata.Name

	// Sweep both kinds — delete-by-label is a no-op when nothing matches.
	for _, resourceType := range []string{"agent", "mcp"} {
		if err := kubernetesDeleteResourcesByDeploymentID(ctx, in.Provider, deploymentID, resourceType, namespace); err != nil {
			return nil, fmt.Errorf("remove %s resources: %w", resourceType, err)
		}
	}

	now := time.Now().UTC()
	gen := in.Deployment.Metadata.Generation
	return &types.RemoveResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "Removed",
			Message:            "kagent resources deleted",
			LastTransitionTime: now,
			ObservedGeneration: gen,
		}},
	}, nil
}

// Logs is not yet implemented for the kubernetes adapter. Returns an
// immediately-closed channel so callers don't block.
func (a *kubernetesDeploymentAdapter) Logs(ctx context.Context, in types.LogsInput) (<-chan types.LogLine, error) {
	ch := make(chan types.LogLine)
	close(ch)
	return ch, nil
}

// Discover enumerates unmanaged kagent/kmcp workloads in the provider's
// namespace and returns them as DiscoveryResult entries. The Syncer (OSS
// or enterprise) persists these into the discovered_kubernetes table.
//
// Rows carrying aregistry.ai/managed=true are skipped because they
// already correspond to existing Deployment rows.
func (a *kubernetesDeploymentAdapter) Discover(ctx context.Context, in types.DiscoverInput) ([]types.DiscoveryResult, error) {
	namespace := kubernetesProviderNamespace(in.Provider)

	isManaged := func(labels map[string]string) bool {
		return labels != nil && labels[kubernetesManagedLabelKey] == "true"
	}

	var out []types.DiscoveryResult
	agents, err := kubernetesListAgents(ctx, in.Provider, namespace)
	if err == nil {
		for _, agent := range agents {
			if isManaged(agent.Labels) {
				continue
			}
			out = append(out, types.DiscoveryResult{
				TargetKind: v1alpha1.KindAgent,
				Namespace:  agent.Namespace,
				Name:       agent.Name,
			})
		}
	}

	mcpServers, err := kubernetesListMCPServers(ctx, in.Provider, namespace)
	if err == nil {
		for _, mcp := range mcpServers {
			if isManaged(mcp.Labels) {
				continue
			}
			out = append(out, types.DiscoveryResult{
				TargetKind: v1alpha1.KindMCPServer,
				Namespace:  mcp.Namespace,
				Name:       mcp.Name,
			})
		}
	}

	remoteMCPs, err := kubernetesListRemoteMCPServers(ctx, in.Provider, namespace)
	if err == nil {
		for _, remote := range remoteMCPs {
			if isManaged(remote.Labels) {
				continue
			}
			out = append(out, types.DiscoveryResult{
				TargetKind: v1alpha1.KindRemoteMCPServer,
				Namespace:  remote.Namespace,
				Name:       remote.Name,
			})
		}
	}

	return out, nil
}

// buildDesiredStateFromV1Alpha1 constructs a *platformtypes.DesiredState from
// the v1alpha1 ApplyInput. Target dispatches by Kind — MCPServer goes
// straight through translate; Agent walks every MCPServers ref via
// in.Getter to build the gateway-free kagent resource graph.
func (a *kubernetesDeploymentAdapter) buildDesiredStateFromV1Alpha1(
	ctx context.Context,
	in types.ApplyInput,
	namespace string,
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
			Namespace:    namespace,
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
			Namespace:    namespace,
			HeaderValues: headerValues,
		})
		if err != nil {
			return nil, err
		}
		return &platformtypes.DesiredState{MCPServers: []*platformtypes.MCPServer{server}}, nil
	case *v1alpha1.Agent:
		agent, servers, err := utils.SpecToPlatformAgent(ctx, target.Metadata, target.Spec, utils.AgentTranslateOpts{
			DeploymentID:  deploymentID,
			Namespace:     namespace,
			KagentURL:     "http://kagent-controller.kagent.svc.cluster.local",
			DeploymentEnv: envValues,
			HeaderValues:  headerValues,
			Getter:        in.Getter,
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

// namespaceFromV1Alpha1 picks the target kubernetes namespace:
//  1. Deployment.Spec.Env[KAGENT_NAMESPACE] (user override).
//  2. Provider.Spec.Config.namespace.
//  3. Ambient kubeconfig default.
func namespaceFromV1Alpha1(deployment *v1alpha1.Deployment, provider *v1alpha1.Provider) string {
	if deployment != nil {
		if ns := strings.TrimSpace(deployment.Spec.Env[constants.EnvKagentNamespace]); ns != "" {
			return ns
		}
	}
	if ns := kubernetesProviderNamespace(provider); ns != "" {
		return ns
	}
	return kubernetesDefaultNamespace()
}

// Compile-time assertion that the kubernetes adapter satisfies the v1alpha1
// DeploymentAdapter contract.
var _ types.DeploymentAdapter = (*kubernetesDeploymentAdapter)(nil)
