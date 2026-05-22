//go:build integration

package controller

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/stretchr/testify/require"
)

func TestDeploymentController_DerivesAndExecutesApply(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-deploy", v1alpha1.DesiredStateDeployed)

	sources := NewSourceIndex(stores)
	require.NoError(t, sources.Refresh(ctx))
	deriver := &DeploymentWorkDeriver{Sources: sources, Work: workStore}
	_, err := deriver.DeriveAll(ctx)
	require.NoError(t, err)
	_, err = deriver.DeriveAll(ctx)
	require.NoError(t, err, "duplicate derivation should coalesce by work key")

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	got := loadDeployment(t, stores, deployment.Metadata.Name)
	ready := got.Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionTrue, ready.Status)
	require.Equal(t, deployment.Metadata.Generation, ready.ObservedGeneration)

	history, err := eventStore.ListByWorkKey(ctx, deploymentWorkKey(
		v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: deployment.Metadata.Name},
		deployment.Metadata.UID, deployment.Metadata.Generation, ReconcileActionApply), 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "success", history[0].Outcome)
}

func TestDeploymentController_BlocksMissingTargetWithoutAdapterCall(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedDeployment(t, stores, "missing-target", v1alpha1.DesiredStateDeployed)

	sources := NewSourceIndex(stores)
	require.NoError(t, sources.Refresh(ctx))
	deriver := &DeploymentWorkDeriver{Sources: sources, Work: workStore}
	_, err := deriver.DeriveAll(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.applyCalls.Load())

	got := loadDeployment(t, stores, "missing-target")
	ready := got.Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionFalse, ready.Status)
	require.Equal(t, "ReferencePending", ready.Reason)
}

func TestDeploymentController_DeleteWaitsForRemoveThenClearsFinalizer(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-me", v1alpha1.DesiredStateDeployed)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))
	terminating := loadDeployment(t, stores, deployment.Metadata.Name)
	require.NotNil(t, terminating.Metadata.DeletionTimestamp)

	sources := NewSourceIndex(stores)
	require.NoError(t, sources.Refresh(ctx))
	deriver := &DeploymentWorkDeriver{Sources: sources, Work: workStore}
	_, err := deriver.DeriveAll(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.removeCalls.Load())

	purged, err := stores[v1alpha1.KindDeployment].PurgeFinalized(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(1), purged)
}

func TestDeploymentController_SkipsStaleGenerationWork(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "stale", v1alpha1.DesiredStateDeployed)

	work, err := DeriveDeploymentWork(deployment)
	require.NoError(t, err)
	work.NextAttemptAt = time.Now().Add(-time.Minute)
	require.NoError(t, workStore.Upsert(ctx, work))

	deployment.Spec.RuntimeConfig = map[string]any{"changed": true}
	_, err = stores[v1alpha1.KindDeployment].Upsert(ctx, deployment)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.applyCalls.Load())

	history, err := eventStore.ListByWorkKey(ctx, work.Key, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "stale", history[0].Outcome)
}

func newControllerTestStores(t *testing.T) (map[string]*v1alpha1store.Store, *v1alpha1store.ReconcileWorkStore, *v1alpha1store.ReconcileEventStore) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)
	return stores, v1alpha1store.NewReconcileWorkStore(pool), v1alpha1store.NewReconcileEventStore(pool)
}

func newTestExecutor(
	stores map[string]*v1alpha1store.Store,
	workStore *v1alpha1store.ReconcileWorkStore,
	eventStore *v1alpha1store.ReconcileEventStore,
	adapter types.DeploymentAdapter,
) *DeploymentExecutor {
	return &DeploymentExecutor{
		Stores:        stores,
		Adapters:      map[string]types.DeploymentAdapter{"Local": adapter},
		Getter:        internaldb.NewGetter(stores),
		Work:          workStore,
		Events:        eventStore,
		Owner:         "test",
		LeaseDuration: time.Minute,
		BackoffDelay:  time.Millisecond,
	}
}

func seedRuntime(t *testing.T, stores map[string]*v1alpha1store.Store, name string) {
	t.Helper()
	_, err := stores[v1alpha1.KindRuntime].Upsert(context.Background(), &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec:     v1alpha1.RuntimeSpec{Type: "Local"},
	})
	require.NoError(t, err)
}

func seedMCPServer(t *testing.T, stores map[string]*v1alpha1store.Store, name string) {
	t.Helper()
	_, err := stores[v1alpha1.KindMCPServer].Upsert(context.Background(), &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.MCPServerSpec{
			Description: "test",
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					RegistryType: v1alpha1.RegistryTypeOCI,
					Identifier:   "ghcr.io/example/weather:1.0.0",
					Transport:    v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	})
	require.NoError(t, err)
}

func seedDeployment(t *testing.T, stores map[string]*v1alpha1store.Store, name, desiredState string) *v1alpha1.Deployment {
	t.Helper()
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Tag: v1alpha1store.DefaultTag()},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "local"},
			DesiredState: desiredState,
		},
	}
	_, err := stores[v1alpha1.KindDeployment].Upsert(context.Background(), deployment, v1alpha1store.UpsertOpts{
		InitialFinalizers: []string{DeploymentControllerFinalizer},
	})
	require.NoError(t, err)
	return loadDeployment(t, stores, name)
}

func loadDeployment(t *testing.T, stores map[string]*v1alpha1store.Store, name string) *v1alpha1.Deployment {
	t.Helper()
	raw, err := stores[v1alpha1.KindDeployment].GetLatestIncludingTerminating(context.Background(), "default", name)
	require.NoError(t, err)
	deployment, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Deployment {
		return &v1alpha1.Deployment{}
	}, raw, v1alpha1.KindDeployment)
	require.NoError(t, err)
	return deployment
}

type recordingDeploymentAdapter struct {
	applyCalls  atomic.Int32
	removeCalls atomic.Int32
}

func (a *recordingDeploymentAdapter) Type() string { return "Local" }

func (a *recordingDeploymentAdapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindMCPServer, v1alpha1.KindAgent}
}

func (a *recordingDeploymentAdapter) Apply(context.Context, types.ApplyInput) (*types.ApplyResult, error) {
	a.applyCalls.Add(1)
	return &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Ready",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Applied",
			ObservedGeneration: 1,
		}},
	}, nil
}

func (a *recordingDeploymentAdapter) Remove(context.Context, types.RemoveInput) (*types.RemoveResult, error) {
	a.removeCalls.Add(1)
	return &types.RemoveResult{
		Conditions: []v1alpha1.Condition{{
			Type:   "Ready",
			Status: v1alpha1.ConditionFalse,
			Reason: "Removed",
		}},
	}, nil
}

func (a *recordingDeploymentAdapter) Logs(context.Context, types.LogsInput) (<-chan types.LogLine, error) {
	ch := make(chan types.LogLine)
	close(ch)
	return ch, nil
}

func (a *recordingDeploymentAdapter) Discover(context.Context, types.DiscoverInput) ([]types.DiscoveryResult, error) {
	return nil, nil
}
