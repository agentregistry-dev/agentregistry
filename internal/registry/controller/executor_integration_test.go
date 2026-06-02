//go:build integration

package controller

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestDeploymentController_DerivesAndExecutesApply(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-deploy", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)
	_, err = controller.FullReconcile(ctx)
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

func TestDeploymentController_SkipsUnchangedApplyAfterRepairReconcile(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-stable", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	applied := loadDeployment(t, stores, deployment.Metadata.Name)
	var details deploymentControllerDetails
	ok, err := applied.Status.GetDetailsKey(deploymentControllerDetailsKey, &details)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, details.LastAppliedFingerprint)

	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)
	processed, err = executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load(), "unchanged desired input must not call the adapter again")

	history, err := eventStore.ListByWorkKey(ctx, deploymentWorkKey(
		v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: deployment.Metadata.Name},
		deployment.Metadata.UID, deployment.Metadata.Generation, ReconcileActionApply), 10)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "unchanged", history[0].Outcome)
	require.Equal(t, "success", history[1].Outcome)
}

func TestDeploymentController_RepairResyncsDoNotReplayUnchangedApply(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-no-storm", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	controller.Events = fakeEventReader{}
	_, err := controller.Refresh(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	const repairTicks = 5
	for range repairTicks {
		result, err := controller.Refresh(ctx)
		require.NoError(t, err)
		require.True(t, result.FullResynced)

		processed, err = executor.RunOnce(ctx, 10)
		require.NoError(t, err)
		require.Equal(t, 1, processed)
		require.Equal(t, int32(1), adapter.applyCalls.Load(), "repair resync must complete work without calling adapter.Apply again")
	}

	history, err := eventStore.ListByWorkKey(ctx, deploymentWorkKey(
		v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: deployment.Metadata.Name},
		deployment.Metadata.UID, deployment.Metadata.Generation, ReconcileActionApply), 10)
	require.NoError(t, err)
	require.Len(t, history, repairTicks+1)
	successes := 0
	unchanged := 0
	for _, event := range history {
		switch event.Outcome {
		case "success":
			successes++
		case "unchanged":
			unchanged++
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, repairTicks, unchanged)
}

func TestDeploymentController_ForceAnnotationBypassesUnchangedApplyOnce(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-force", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	require.NoError(t, stores[v1alpha1.KindDeployment].PatchAnnotations(ctx, "default", deployment.Metadata.Name, "", func(current map[string]string) map[string]string {
		current[DeploymentForceAnnotation] = "manual-1"
		return current
	}))
	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)
	processed, err = executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(2), adapter.applyCalls.Load())

	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)
	processed, err = executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(2), adapter.applyCalls.Load(), "the same force token must not force every resync forever")

	got := loadDeployment(t, stores, deployment.Metadata.Name)
	var details deploymentControllerDetails
	ok, err := got.Status.GetDetailsKey(deploymentControllerDetailsKey, &details)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "manual-1", details.LastForceToken)
}

func TestDeploymentController_BlocksMissingTargetWithoutAdapterCall(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedDeployment(t, stores, "missing-target", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
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

func TestDeploymentController_ReappliesWhenMissingTargetAppears(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	deployment := seedDeployment(t, stores, "target-later", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.applyCalls.Load())

	seedMCPServer(t, stores, "weather")
	_, err = controller.HandleEvent(ctx, v1alpha1store.ControlPlaneEvent{
		Key: v1alpha1store.ResourceKey{
			Kind:      v1alpha1.KindMCPServer,
			Namespace: "default",
			Name:      "weather",
			Tag:       v1alpha1store.DefaultTag(),
		},
		Operation: "update",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		processed, err = executor.RunOnce(ctx, 10)
		return err == nil && adapter.applyCalls.Load() == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	history, err := eventStore.ListByWorkKey(ctx, deploymentWorkKey(
		v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: deployment.Metadata.Name},
		deployment.Metadata.UID, deployment.Metadata.Generation, ReconcileActionApply), 10)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "success", history[0].Outcome)
	require.Equal(t, "blocked", history[1].Outcome)
}

func TestDeploymentController_ReappliesAgentDeploymentWhenReferencedMCPServerChanges(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServerWithIdentifier(t, stores, "weather", "ghcr.io/example/weather:1.0.0")
	seedAgent(t, stores, "assistant", []v1alpha1.ResourceRef{{Name: "weather"}})
	deployment := seedAgentDeployment(t, stores, "assistant-deploy", "assistant", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	seedMCPServerWithIdentifier(t, stores, "weather", "ghcr.io/example/weather:2.0.0")
	_, err = controller.HandleEvent(ctx, v1alpha1store.ControlPlaneEvent{
		Key: v1alpha1store.ResourceKey{
			Kind:      v1alpha1.KindMCPServer,
			Namespace: "default",
			Name:      "weather",
			Tag:       v1alpha1store.DefaultTag(),
		},
		Operation: "insert",
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		processed, err = executor.RunOnce(ctx, 10)
		return err == nil && adapter.applyCalls.Load() == 2
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 1, processed)

	history, err := eventStore.ListByWorkKey(ctx, deploymentWorkKey(
		v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: deployment.Metadata.Name},
		deployment.Metadata.UID, deployment.Metadata.Generation, ReconcileActionApply), 10)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "success", history[0].Outcome)
	require.Equal(t, "success", history[1].Outcome)
}

func TestDeploymentController_DeleteWaitsForRemoveThenPurgesFinalizedRow(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-me", v1alpha1.DesiredStateDeployed)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))
	terminating := loadDeployment(t, stores, deployment.Metadata.Name)
	require.NotNil(t, terminating.Metadata.DeletionTimestamp)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.removeCalls.Load())

	requireDeploymentMissing(t, stores, deployment.Metadata.Name)

	_, err = stores[v1alpha1.KindDeployment].Upsert(ctx, &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: deployment.Metadata.Name},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Tag: v1alpha1store.DefaultTag()},
			RuntimeRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "local"},
		},
	}, v1alpha1store.UpsertOpts{InitialFinalizers: []string{DeploymentControllerFinalizer}})
	require.NoError(t, err, "finalized deletes must not block same-name apply")
}

func TestDeploymentController_RemoveFailureKeepsFinalizerAndRetries(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "remove-retry", v1alpha1.DesiredStateDeployed)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))
	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{removeErr: errors.New("temporary remove failure")}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	executor.Now = func() time.Time { return time.Now().Add(-time.Minute) }
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.removeCalls.Load())

	terminating := loadDeployment(t, stores, deployment.Metadata.Name)
	require.NotNil(t, terminating.Metadata.DeletionTimestamp)
	require.Contains(t, loadDeploymentFinalizers(t, stores, deployment.Metadata.Name), DeploymentControllerFinalizer)
	purged, err := stores[v1alpha1.KindDeployment].PurgeFinalized(ctx)
	require.NoError(t, err)
	require.Zero(t, purged)

	workKey := deploymentWorkKey(
		v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: deployment.Metadata.Name},
		deployment.Metadata.UID, deployment.Metadata.Generation, ReconcileActionDelete)
	history, err := eventStore.ListByWorkKey(ctx, workKey, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "error", history[0].Outcome)
	require.Contains(t, history[0].Error, "temporary remove failure")

	adapter.removeErr = nil
	processed, err = executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(2), adapter.removeCalls.Load())

	requireDeploymentMissing(t, stores, deployment.Metadata.Name)

	history, err = eventStore.ListByWorkKey(ctx, workKey, 10)
	require.NoError(t, err)
	require.Len(t, history, 2)
	require.Equal(t, "success", history[0].Outcome)
	require.Equal(t, "error", history[1].Outcome)
}

func TestDeploymentController_DeleteAbandonsPendingApplyWork(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-with-apply-pending", v1alpha1.DesiredStateDeployed)

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))
	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.applyCalls.Load(), "pending apply work must not run after delete")
	require.Equal(t, int32(1), adapter.removeCalls.Load())
	requireDeploymentMissing(t, stores, deployment.Metadata.Name)
}

func TestDeploymentController_SkipsClaimedApplyWhenDeploymentIsDeleted(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-with-apply-claimed", v1alpha1.DesiredStateDeployed)

	work, err := deriveDeploymentWork(deployment)
	require.NoError(t, err)
	work.NextAttemptAt = time.Now().Add(-time.Minute)
	require.NoError(t, workStore.Upsert(ctx, work))
	claimed, err := workStore.ClaimDue(ctx, "worker-a", time.Now().Add(time.Minute), 1)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, string(ReconcileActionApply), claimed[0].Action)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	require.NoError(t, executor.executeClaim(ctx, claimed[0]))
	require.Zero(t, adapter.applyCalls.Load(), "claimed apply work must be stale once the deployment is terminating")

	history, err := eventStore.ListByWorkKey(ctx, work.Key, 10)
	require.NoError(t, err)
	require.Len(t, history, 1)
	require.Equal(t, "stale", history[0].Outcome)
}

func TestDeploymentController_DeleteFinalizesWhenRuntimeRefMissing(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-missing-runtime", v1alpha1.DesiredStateDeployed)
	require.NoError(t, stores[v1alpha1.KindRuntime].Delete(ctx, "default", "local", ""))
	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))

	controller := newDeploymentTestController(stores, workStore)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	adapter := &recordingDeploymentAdapter{}
	executor := newTestExecutor(stores, workStore, eventStore, adapter)
	processed, err := executor.RunOnce(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.removeCalls.Load(), "missing runtime cannot dispatch adapter remove")
	requireDeploymentMissing(t, stores, deployment.Metadata.Name)
}

func TestDeploymentController_SkipsStaleGenerationWork(t *testing.T) {
	ctx := context.Background()
	stores, workStore, eventStore := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "stale", v1alpha1.DesiredStateDeployed)

	work, err := deriveDeploymentWork(deployment)
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

func newDeploymentTestController(
	stores map[string]*v1alpha1store.Store,
	workStore *v1alpha1store.ReconcileWorkStore,
) *DeploymentController {
	return &DeploymentController{Stores: stores, Work: workStore}
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
	seedMCPServerWithIdentifier(t, stores, name, "ghcr.io/example/weather:1.0.0")
}

func seedMCPServerWithIdentifier(t *testing.T, stores map[string]*v1alpha1store.Store, name, identifier string) {
	t.Helper()
	_, err := stores[v1alpha1.KindMCPServer].Upsert(context.Background(), &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.MCPServerSpec{
			Description: "test",
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					RegistryType: v1alpha1.RegistryTypeOCI,
					Identifier:   identifier,
					Transport:    v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	})
	require.NoError(t, err)
}

func seedAgent(t *testing.T, stores map[string]*v1alpha1store.Store, name string, mcpServers []v1alpha1.ResourceRef) {
	t.Helper()
	_, err := stores[v1alpha1.KindAgent].Upsert(context.Background(), &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.AgentSpec{
			Title:      "test agent",
			MCPServers: mcpServers,
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

func seedAgentDeployment(t *testing.T, stores map[string]*v1alpha1store.Store, name, agentName, desiredState string) *v1alpha1.Deployment {
	t.Helper()
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: agentName, Tag: v1alpha1store.DefaultTag()},
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

func requireDeploymentMissing(t *testing.T, stores map[string]*v1alpha1store.Store, name string) {
	t.Helper()
	_, err := stores[v1alpha1.KindDeployment].GetLatestIncludingTerminating(context.Background(), "default", name)
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func loadDeploymentFinalizers(t *testing.T, stores map[string]*v1alpha1store.Store, name string) []string {
	t.Helper()
	var finalizers []string
	err := stores[v1alpha1.KindDeployment].PatchFinalizers(context.Background(), "default", name, "", func(current []string) []string {
		finalizers = append([]string(nil), current...)
		return current
	})
	require.NoError(t, err)
	return finalizers
}

type recordingDeploymentAdapter struct {
	applyCalls  atomic.Int32
	removeCalls atomic.Int32
	applyErr    error
	removeErr   error
}

func (a *recordingDeploymentAdapter) Type() string { return "Local" }

func (a *recordingDeploymentAdapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindMCPServer, v1alpha1.KindAgent}
}

func (a *recordingDeploymentAdapter) Apply(context.Context, types.ApplyInput) (*types.ApplyResult, error) {
	a.applyCalls.Add(1)
	if a.applyErr != nil {
		return nil, a.applyErr
	}
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
	if a.removeErr != nil {
		return nil, a.removeErr
	}
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
