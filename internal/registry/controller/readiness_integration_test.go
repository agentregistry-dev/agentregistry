//go:build integration

package controller

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// probingDeploymentAdapter mimics the Kubernetes adapter: Apply only reports
// Progressing, and readiness comes from ProbeReadiness.
type probingDeploymentAdapter struct {
	recordingDeploymentAdapter
	workloadAvailable atomic.Bool
	probeCalls        atomic.Int32
}

func (a *probingDeploymentAdapter) Apply(_ context.Context, input types.ApplyInput) (*types.ApplyResult, error) {
	a.applyCalls.Add(1)
	return &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type:               "Progressing",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Applied",
			Message:            "waiting for rollout",
			ObservedGeneration: input.Deployment.Metadata.Generation,
		}},
	}, nil
}

func (a *probingDeploymentAdapter) ProbeReadiness(_ context.Context, input types.ApplyInput) (*types.ReadinessResult, error) {
	a.probeCalls.Add(1)
	gen := input.Deployment.Metadata.Generation
	if !a.workloadAvailable.Load() {
		return &types.ReadinessResult{
			Ready: false,
			Conditions: []v1alpha1.Condition{{
				Type: "Ready", Status: v1alpha1.ConditionUnknown,
				Reason: "WaitingForWorkload", Message: "0 of 1 updated replicas available",
				ObservedGeneration: gen,
			}},
		}, nil
	}
	return &types.ReadinessResult{
		Ready: true,
		Conditions: []v1alpha1.Condition{{
			Type: "Ready", Status: v1alpha1.ConditionTrue,
			Reason: "WorkloadAvailable", Message: "Kubernetes reports all workloads available",
			ObservedGeneration: gen,
		}, {
			Type: "Progressing", Status: v1alpha1.ConditionFalse,
			Reason: "RolloutComplete", ObservedGeneration: gen,
		}},
	}, nil
}

func waitForQueuedKey(t *testing.T, c *DeploymentController) {
	t.Helper()
	require.Eventually(t, func() bool { return c.workQueue().Len() > 0 },
		2*time.Second, 5*time.Millisecond, "deployment should be requeued for another readiness probe")
}

func TestDeploymentController_SetsReadyOnlyAfterWorkloadAvailable(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-rollout", v1alpha1.DesiredStateDeployed)

	adapter := &probingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	controller.ReadinessRequeueInterval = 20 * time.Millisecond

	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)
	_, err = controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(1), adapter.applyCalls.Load())
	require.Equal(t, int32(1), adapter.probeCalls.Load())

	got := loadDeployment(t, stores, deployment.Metadata.Name)
	ready := got.Status.GetCondition("Ready")
	require.NotNil(t, ready, "Ready must be reported while the workload is rolling out")
	require.Equal(t, v1alpha1.ConditionUnknown, ready.Status, "a converging rollout must not read as failed")
	require.Equal(t, "WaitingForWorkload", ready.Reason)
	progressing := got.Status.GetCondition("Progressing")
	require.NotNil(t, progressing)
	require.Equal(t, v1alpha1.ConditionTrue, progressing.Status)

	// Workload still rolling out: requeued, re-probed, apply not repeated.
	waitForQueuedKey(t, controller)
	_, err = controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(1), adapter.applyCalls.Load())
	require.Equal(t, int32(2), adapter.probeCalls.Load())

	// Kubernetes now reports the workload available.
	adapter.workloadAvailable.Store(true)
	waitForQueuedKey(t, controller)
	_, err = controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(1), adapter.applyCalls.Load(), "readiness polling must not re-apply unchanged input")

	got = loadDeployment(t, stores, deployment.Metadata.Name)
	ready = got.Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionTrue, ready.Status)
	require.Equal(t, "WorkloadAvailable", ready.Reason)
	require.Equal(t, deployment.Metadata.Generation, ready.ObservedGeneration)
	progressing = got.Status.GetCondition("Progressing")
	require.NotNil(t, progressing)
	require.Equal(t, v1alpha1.ConditionFalse, progressing.Status)

	// Converged: no further polling requeue.
	time.Sleep(5 * controller.ReadinessRequeueInterval)
	require.Equal(t, 0, controller.workQueue().Len())
}

// A Deployment applied by an unpatched server is stuck at Progressing with its
// fingerprint already recorded. The next resync must set Ready=True from the
// probe without calling Apply again.
func TestDeploymentController_RepairsStuckProgressingDeploymentOnResync(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-stuck", v1alpha1.DesiredStateDeployed)

	// Simulate the old behavior: apply with an adapter that cannot probe.
	legacy := &probingDeploymentAdapter{}
	legacyController := newDeploymentTestController(stores, &legacyApplyOnly{inner: legacy})
	_, err := legacyController.FullReconcile(ctx)
	require.NoError(t, err)
	_, err = legacyController.RunOnce(ctx)
	require.NoError(t, err)
	require.Nil(t, loadDeployment(t, stores, deployment.Metadata.Name).Status.GetCondition("Ready"))

	adapter := &probingDeploymentAdapter{}
	adapter.workloadAvailable.Store(true)
	controller := newDeploymentTestController(stores, adapter)
	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)
	_, err = controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(0), adapter.applyCalls.Load())
	require.Equal(t, int32(1), adapter.probeCalls.Load())

	ready := loadDeployment(t, stores, deployment.Metadata.Name).Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionTrue, ready.Status)
}

// legacyApplyOnly hides ProbeReadiness so the controller behaves as v0.4.0 did.
type legacyApplyOnly struct{ inner *probingDeploymentAdapter }

func (a *legacyApplyOnly) Type() string                   { return a.inner.Type() }
func (a *legacyApplyOnly) SupportedTargetKinds() []string { return a.inner.SupportedTargetKinds() }
func (a *legacyApplyOnly) Apply(ctx context.Context, in types.ApplyInput) (*types.ApplyResult, error) {
	return a.inner.Apply(ctx, in)
}
func (a *legacyApplyOnly) Remove(ctx context.Context, in types.RemoveInput) (*types.RemoveResult, error) {
	return a.inner.Remove(ctx, in)
}
func (a *legacyApplyOnly) Logs(ctx context.Context, in types.LogsInput) (<-chan types.LogLine, error) {
	return a.inner.Logs(ctx, in)
}
