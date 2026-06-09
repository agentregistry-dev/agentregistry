//go:build integration

package controller

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestDeploymentDiscoveryController_MaterializesDiscoveredDeployment(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
		RuntimeMetadata: map[string]string{
			"remoteId": "agent-123",
		},
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, DeploymentDiscoverySyncResult{Runtimes: 1, Discovered: 1}, result)

	name := discoveredDeploymentName("local", v1alpha1.KindAgent, "external-agent", "unknown", "default")
	require.True(t, strings.HasPrefix(name, "discovered-external-agent-"))
	deployment := loadDeployment(t, stores, name)
	require.Equal(t, v1alpha1.DeploymentOriginDiscovered, deployment.Metadata.Annotations[v1alpha1.DeploymentOriginAnnotation])
	require.Equal(t, "local", deployment.Metadata.Annotations[v1alpha1.DeploymentDiscoveredRuntimeAnnotation])
	require.Equal(t, "Local", deployment.Metadata.Annotations[v1alpha1.DeploymentDiscoveredRuntimeTypeAnnotation])
	require.Equal(t, v1alpha1.KindAgent, deployment.Spec.TargetRef.Kind)
	require.Equal(t, "external-agent", deployment.Spec.TargetRef.Name)
	require.Equal(t, "unknown", deployment.Spec.TargetRef.Tag)
	require.Equal(t, "local", deployment.Spec.RuntimeRef.Name)
	require.Equal(t, v1alpha1.ConditionTrue, deployment.Status.GetCondition("Ready").Status)
	require.Equal(t, v1alpha1.ConditionTrue, deployment.Status.GetCondition(deploymentDiscoveryCondition).Status)

	var runtimeMetadata map[string]string
	ok, err := deployment.Status.GetDetailsKey(deploymentRuntimeDetailsKey, &runtimeMetadata)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "agent-123", runtimeMetadata["remoteId"])
}

func TestDeploymentDiscoveryController_MarksMissingRowsStale(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	adapter.results = nil
	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Stale)

	name := discoveredDeploymentName("local", v1alpha1.KindAgent, "external-agent", "unknown", "default")
	deployment := loadDeployment(t, stores, name)
	condition := deployment.Status.GetCondition(deploymentDiscoveryCondition)
	require.NotNil(t, condition)
	require.Equal(t, v1alpha1.ConditionFalse, condition.Status)
	require.Equal(t, "ProviderMissing", condition.Reason)
}

func TestDeploymentDiscoveryController_ErrorDoesNotMarkRowsStale(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	adapter.results = nil
	adapter.err = errors.New("provider unavailable")
	result, err := discovery.Sync(ctx)
	require.Error(t, err)
	require.Zero(t, result.Stale)

	name := discoveredDeploymentName("local", v1alpha1.KindAgent, "external-agent", "unknown", "default")
	deployment := loadDeployment(t, stores, name)
	condition := deployment.Status.GetCondition(deploymentDiscoveryCondition)
	require.NotNil(t, condition)
	require.Equal(t, v1alpha1.ConditionTrue, condition.Status)
}

func TestDeploymentDiscoveryController_SkipsAdaptersWithoutDiscoverySource(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	discovery := newDeploymentDiscoveryTestController(stores, &lifecycleOnlyDiscoveryTestAdapter{})

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, DeploymentDiscoverySyncResult{}, result)

	deployments, cursor, err := stores[v1alpha1.KindDeployment].List(ctx, v1alpha1store.ListOpts{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, cursor)
	require.Empty(t, deployments)
}

func TestDeploymentDiscoveryController_DedupesManagedDeploymentTargets(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	seedDeployment(t, stores, "managed-agent", v1alpha1.DesiredStateDeployed)
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindMCPServer,
		Name:       "weather",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Runtimes)
	require.Zero(t, result.Discovered)
}

func TestDeploymentController_SkipsDiscoveredRows(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	reconcileAdapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, reconcileAdapter)
	count, err := controller.FullReconcile(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Zero(t, controller.workQueue().Len())

	name := discoveredDeploymentName("local", v1alpha1.KindAgent, "external-agent", "unknown", "default")
	controller.workQueue().Add(deploymentQueueKey{Namespace: "default", Name: name})
	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, reconcileAdapter.applyCalls.Load())
	require.Zero(t, reconcileAdapter.removeCalls.Load())
	require.Empty(t, loadDeploymentFinalizers(t, stores, name))
}

func newDeploymentDiscoveryTestController(
	stores map[string]*v1alpha1store.Store,
	adapter types.DeploymentAdapter,
) *DeploymentDiscoveryController {
	return &DeploymentDiscoveryController{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{"Local": adapter},
	}
}

type discoveryTestAdapter struct {
	results []types.DiscoveryResult
	err     error
}

func (a *discoveryTestAdapter) Type() string { return "Local" }

func (a *discoveryTestAdapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindMCPServer, v1alpha1.KindAgent}
}

func (a *discoveryTestAdapter) Apply(context.Context, types.ApplyInput) (*types.ApplyResult, error) {
	return &types.ApplyResult{}, nil
}

func (a *discoveryTestAdapter) Remove(context.Context, types.RemoveInput) (*types.RemoveResult, error) {
	return &types.RemoveResult{}, nil
}

func (a *discoveryTestAdapter) Logs(context.Context, types.LogsInput) (<-chan types.LogLine, error) {
	ch := make(chan types.LogLine)
	close(ch)
	return ch, nil
}

func (a *discoveryTestAdapter) Discover(context.Context, types.DiscoverInput) ([]types.DiscoveryResult, error) {
	if a.err != nil {
		return nil, a.err
	}
	return a.results, nil
}

type lifecycleOnlyDiscoveryTestAdapter struct{}

func (a *lifecycleOnlyDiscoveryTestAdapter) Type() string { return "Local" }

func (a *lifecycleOnlyDiscoveryTestAdapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindMCPServer, v1alpha1.KindAgent}
}

func (a *lifecycleOnlyDiscoveryTestAdapter) Apply(context.Context, types.ApplyInput) (*types.ApplyResult, error) {
	return &types.ApplyResult{}, nil
}

func (a *lifecycleOnlyDiscoveryTestAdapter) Remove(context.Context, types.RemoveInput) (*types.RemoveResult, error) {
	return &types.RemoveResult{}, nil
}

func (a *lifecycleOnlyDiscoveryTestAdapter) Logs(context.Context, types.LogsInput) (<-chan types.LogLine, error) {
	ch := make(chan types.LogLine)
	close(ch)
	return ch, nil
}
