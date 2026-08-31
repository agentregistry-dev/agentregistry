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

const discoveryTestRuntimeName = "test-runtime"

func TestDeploymentDiscoveryController_MaterializesDiscoveredDeployment(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
		RuntimeMetadata: map[string]string{
			types.RuntimeMetadataRemoteIDKey: "agent-123",
		},
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, DeploymentDiscoverySyncResult{Runtimes: 1, Discovered: 1}, result)

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")
	require.True(t, strings.HasPrefix(name, "discovered-external-agent-"))
	deployment := loadDeployment(t, stores, name)
	require.Equal(t, v1alpha1.DeploymentOriginDiscovered, deployment.Metadata.Annotations[v1alpha1.DeploymentOriginAnnotation])
	require.Equal(t, discoveryTestRuntimeName, deployment.Metadata.Annotations[v1alpha1.DeploymentDiscoveredRuntimeAnnotation])
	require.Equal(t, "Test", deployment.Metadata.Annotations[v1alpha1.DeploymentDiscoveredRuntimeTypeAnnotation])
	require.Equal(t, v1alpha1.KindAgent, deployment.Spec.TargetRef.Kind)
	require.Equal(t, "external-agent", deployment.Spec.TargetRef.Name)
	require.Equal(t, "unknown", deployment.Spec.TargetRef.Tag)
	require.Equal(t, discoveryTestRuntimeName, deployment.Spec.RuntimeRef.Name)
	require.Equal(t, v1alpha1.ConditionTrue, deployment.Status.GetCondition("Ready").Status)
	require.Equal(t, v1alpha1.ConditionTrue, deployment.Status.GetCondition(deploymentDiscoveryCondition).Status)

	var runtimeMetadata map[string]string
	ok, err := deployment.Status.GetDetailsKey(deploymentRuntimeDetailsKey, &runtimeMetadata)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "agent-123", runtimeMetadata[types.RuntimeMetadataRemoteIDKey])
}

func TestDeploymentDiscoveryController_MarksRowsStaleAfterConsecutiveMisses(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")

	// Misses below the staleness threshold only bump the counter; the
	// conditions stay True (provider list APIs are eventually consistent).
	adapter.results = nil
	for miss := 1; miss < defaultDeploymentDiscoveryStaleAfterMisses; miss++ {
		result, err := discovery.Sync(ctx)
		require.NoError(t, err)
		require.Zero(t, result.Stale, "miss %d should not mark the row stale", miss)
		require.Zero(t, result.Removed)
		deployment := loadDeployment(t, stores, name)
		require.Equal(t, v1alpha1.ConditionTrue, deployment.Status.GetCondition(deploymentDiscoveryCondition).Status)
		require.Equal(t, miss, discoveredMissCount(deployment))
	}

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Stale)
	require.Zero(t, result.Removed)

	deployment := loadDeployment(t, stores, name)
	condition := deployment.Status.GetCondition(deploymentDiscoveryCondition)
	require.NotNil(t, condition)
	require.Equal(t, v1alpha1.ConditionFalse, condition.Status)
	require.Equal(t, "ProviderMissing", condition.Reason)
	ready := deployment.Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionFalse, ready.Status)
}

func TestDeploymentDiscoveryController_DeletesRowsAfterRepeatedMisses(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")

	adapter.results = nil
	for miss := 1; miss < defaultDeploymentDiscoveryDeleteAfterMisses; miss++ {
		result, err := discovery.Sync(ctx)
		require.NoError(t, err)
		require.Zero(t, result.Removed, "miss %d should not delete the row", miss)
		loadDeployment(t, stores, name)
	}

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Removed)
	requireDeploymentMissing(t, stores, name)
}

func TestDeploymentDiscoveryController_UsesConfiguredMissThresholds(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	discovery.StaleAfterMisses = 1
	discovery.DeleteAfterMisses = 2
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")

	adapter.results = nil
	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Stale)
	require.Zero(t, result.Removed)
	deployment := loadDeployment(t, stores, name)
	require.Equal(t, 1, discoveredMissCount(deployment))
	require.Equal(t, v1alpha1.ConditionFalse, deployment.Status.GetCondition(deploymentDiscoveryCondition).Status)

	result, err = discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Removed)
	requireDeploymentMissing(t, stores, name)
}

func TestDeploymentDiscoveryController_DeletesRowsWhenRuntimeRemoved(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	adapter := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	require.NoError(t, stores[v1alpha1.KindRuntime].Delete(ctx, "default", discoveryTestRuntimeName, ""))

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Removed)
	require.Zero(t, result.Stale)

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")
	requireDeploymentMissing(t, stores, name)
}

func TestDeploymentDiscoveryController_ReobservedRowResetsMissCounter(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	results := []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "external-agent",
	}}
	adapter := &discoveryTestAdapter{results: results}
	discovery := newDeploymentDiscoveryTestController(stores, adapter)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")

	// Two misses, then the workload reappears: the counter must reset so the
	// next miss streak starts from scratch.
	adapter.results = nil
	for range 2 {
		_, err := discovery.Sync(ctx)
		require.NoError(t, err)
	}
	require.Equal(t, 2, discoveredMissCount(loadDeployment(t, stores, name)))

	adapter.results = results
	_, err = discovery.Sync(ctx)
	require.NoError(t, err)
	deployment := loadDeployment(t, stores, name)
	require.Zero(t, discoveredMissCount(deployment))
	require.Equal(t, v1alpha1.ConditionTrue, deployment.Status.GetCondition(deploymentDiscoveryCondition).Status)

	adapter.results = nil
	for range 2 {
		_, err := discovery.Sync(ctx)
		require.NoError(t, err)
	}
	deployment = loadDeployment(t, stores, name)
	require.Equal(t, 2, discoveredMissCount(deployment))
	require.Equal(t, v1alpha1.ConditionTrue, deployment.Status.GetCondition(deploymentDiscoveryCondition).Status)
}

func TestDeploymentDiscoveryController_ErrorDoesNotMarkRowsStale(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
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

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")
	deployment := loadDeployment(t, stores, name)
	condition := deployment.Status.GetCondition(deploymentDiscoveryCondition)
	require.NotNil(t, condition)
	require.Equal(t, v1alpha1.ConditionTrue, condition.Status)
	require.Zero(t, discoveredMissCount(deployment), "errored polls must not count as misses")
}

func TestDeploymentDiscoveryController_SkipsRuntimeWithoutDiscoverySource(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	discovery := &DeploymentDiscoveryController{Stores: stores}

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

func TestDeploymentDiscoveryController_PreservesNameForRemoteID(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	source := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind:      v1alpha1.KindAgent,
		Name:            "original-name",
		RuntimeMetadata: map[string]string{types.RuntimeMetadataRemoteIDKey: "agent-123"},
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, source)
	_, err := discovery.Sync(ctx)
	require.NoError(t, err)

	originalName := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "original-name", "unknown", "default")
	original := loadDeployment(t, stores, originalName)
	source.results[0].Name = "renamed-agent"
	_, err = discovery.Sync(ctx)
	require.NoError(t, err)

	adopted := loadDeployment(t, stores, originalName)
	require.Equal(t, original.Metadata.UID, adopted.Metadata.UID)
	require.Equal(t, "renamed-agent", adopted.Spec.TargetRef.Name)
	renamed := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "renamed-agent", "unknown", "default")
	requireDeploymentMissing(t, stores, renamed)
}

func TestDeploymentDiscoveryController_DedupesManagedDeploymentRemoteID(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedAgentDeployment(t, stores, "managed-agent", "managed-agent", v1alpha1.DesiredStateDeployed)
	managed := loadDeployment(t, stores, "managed-agent")
	require.NoError(t, stores[v1alpha1.KindDeployment].PatchStatus(ctx, "default", managed.Metadata.Name, "", v1alpha1.StatusPatcher(func(status *v1alpha1.Status) {
		_ = status.SetDetailsKey(deploymentRuntimeDetailsKey, map[string]string{types.RuntimeMetadataRemoteIDKey: "agent-123"})
	})))
	source := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind:      v1alpha1.KindAgent,
		Name:            "provider-name",
		RuntimeMetadata: map[string]string{types.RuntimeMetadataRemoteIDKey: "agent-123"},
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, source)

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Runtimes)
	require.Zero(t, result.Discovered)
}

func TestDeploymentDiscoveryController_DedupesLegacyManagedDeploymentRemoteID(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedAgentDeployment(t, stores, "managed-agent", "managed-agent", v1alpha1.DesiredStateDeployed)
	managed := loadDeployment(t, stores, "managed-agent")
	require.NoError(t, stores[v1alpha1.KindDeployment].PatchAnnotations(ctx, "default", managed.Metadata.Name, "", func(annotations map[string]string) map[string]string {
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["runtimes.agentregistry.solo.io/test/"+types.RuntimeMetadataRemoteIDKey] = "agent-123"
		return annotations
	}))
	source := &discoveryTestAdapter{results: []types.DiscoveryResult{{
		TargetKind:      v1alpha1.KindAgent,
		Name:            "provider-name",
		RuntimeMetadata: map[string]string{types.RuntimeMetadataRemoteIDKey: "agent-123"},
	}}}
	discovery := newDeploymentDiscoveryTestController(stores, source)

	result, err := discovery.Sync(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, result.Runtimes)
	require.Zero(t, result.Discovered)
}

func TestDeploymentController_SkipsDiscoveredRows(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
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

	name := discoveredDeploymentName(discoveryTestRuntimeName, v1alpha1.KindAgent, "external-agent", "unknown", "default")
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
	source types.DeploymentDiscoverySource,
) *DeploymentDiscoveryController {
	return &DeploymentDiscoveryController{
		Stores:  stores,
		Sources: map[string]types.DeploymentDiscoverySource{"Test": source},
	}
}

type discoveryTestAdapter struct {
	results []types.DiscoveryResult
	err     error
}

func (a *discoveryTestAdapter) Discover(context.Context, types.DiscoverInput) ([]types.DiscoveryResult, error) {
	if a.err != nil {
		return nil, a.err
	}
	return a.results, nil
}
