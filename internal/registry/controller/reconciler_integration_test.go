//go:build integration

package controller

import (
	"context"
	"encoding/json"
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

func TestDeploymentController_EnqueuesAndExecutesApply(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-deploy", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)
	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err, "duplicate scheduling should coalesce by queue key")

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())
	require.Equal(t, int64(deployment.Metadata.Generation), adapter.lastApplyGeneration.Load())

	got := loadDeployment(t, stores, deployment.Metadata.Name)
	ready := got.Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionTrue, ready.Status)
	require.Equal(t, deployment.Metadata.Generation, ready.ObservedGeneration)
}

func TestDeploymentController_SkipsUnchangedApplyAfterRepairReconcile(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-stable", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
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
	processed, err = controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load(), "unchanged desired input must not call the adapter again")
}

func TestDeploymentController_RepairResyncsDoNotReplayUnchangedApply(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	seedDeployment(t, stores, "weather-no-storm", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	controller.Events = fakeEventReader{}
	_, err := controller.Refresh(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	const repairTicks = 5
	for range repairTicks {
		result, err := controller.Refresh(ctx)
		require.NoError(t, err)
		require.True(t, result.FullResynced)

		processed, err = controller.RunOnce(ctx)
		require.NoError(t, err)
		require.Equal(t, 1, processed)
		require.Equal(t, int32(1), adapter.applyCalls.Load(), "repair resync must complete work without calling adapter.Apply again")
	}
}

func TestDeploymentController_ForceAnnotationBypassesUnchangedApplyOnce(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "weather-force", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	require.NoError(t, stores[v1alpha1.KindDeployment].PatchAnnotations(ctx, "default", deployment.Metadata.Name, "", func(current map[string]string) map[string]string {
		current[DeploymentForceAnnotation] = "manual-1"
		return current
	}))
	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)
	processed, err = controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(2), adapter.applyCalls.Load())

	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)
	processed, err = controller.RunOnce(ctx)
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
	stores := newControllerTestStores(t)
	seedDeployment(t, stores, "missing-target", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
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
	stores := newControllerTestStores(t)
	seedDeployment(t, stores, "target-later", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
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
		processed, err = controller.RunOnce(ctx)
		return err == nil && adapter.applyCalls.Load() == 1
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())
}

func TestDeploymentController_ReappliesAgentDeploymentWhenReferencedMCPServerChanges(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServerWithIdentifier(t, stores, "weather", "ghcr.io/example/weather:1.0.0")
	seedAgent(t, stores, "assistant", []v1alpha1.ResourceRef{{Name: "weather"}})
	seedAgentDeployment(t, stores, "assistant-deploy", "assistant", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	applied := loadDeployment(t, stores, "assistant-deploy")
	var firstDetails deploymentControllerDetails
	ok, err := applied.Status.GetDetailsKey(deploymentControllerDetailsKey, &firstDetails)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, firstDetails.Dependencies, 1)
	require.Equal(t, v1alpha1.KindMCPServer, firstDetails.Dependencies[0].Kind)
	require.Equal(t, "default", firstDetails.Dependencies[0].Namespace)
	require.Equal(t, "weather", firstDetails.Dependencies[0].Name)
	require.NotEmpty(t, firstDetails.Dependencies[0].MaterialHash)
	firstDependencyHash := firstDetails.Dependencies[0].MaterialHash

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
		processed, err = controller.RunOnce(ctx)
		return err == nil && adapter.applyCalls.Load() == 2
	}, time.Second, 10*time.Millisecond)
	require.Equal(t, 1, processed)

	reapplied := loadDeployment(t, stores, "assistant-deploy")
	var secondDetails deploymentControllerDetails
	ok, err = reapplied.Status.GetDetailsKey(deploymentControllerDetailsKey, &secondDetails)
	require.NoError(t, err)
	require.True(t, ok)
	require.Len(t, secondDetails.Dependencies, 1)
	require.Equal(t, "weather", secondDetails.Dependencies[0].Name)
	require.NotEqual(t, firstDependencyHash, secondDetails.Dependencies[0].MaterialHash,
		"dependency material hash should change after the referenced MCPServer spec changes")
}

func TestDeploymentController_DeleteWaitsForRemoveThenPurgesFinalizedRow(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-me", v1alpha1.DesiredStateDeployed)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))
	terminating := loadDeployment(t, stores, deployment.Metadata.Name)
	require.NotNil(t, terminating.Metadata.DeletionTimestamp)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.removeCalls.Load())

	requireDeploymentMissing(t, stores, deployment.Metadata.Name)

	_, err = stores[v1alpha1.KindDeployment].Upsert(ctx, &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: deployment.Metadata.Name},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Tag: v1alpha1store.DefaultTag()},
			RuntimeRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "kubernetes-default"},
		},
	}, v1alpha1store.UpsertOpts{InitialFinalizers: []string{DeploymentControllerFinalizer}})
	require.NoError(t, err, "finalized deletes must not block same-name apply")
}

func TestDeploymentController_RemoveFailureKeepsFinalizerAndRetries(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "remove-retry", v1alpha1.DesiredStateDeployed)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))
	adapter := &recordingDeploymentAdapter{removeErr: errors.New("temporary remove failure")}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.removeCalls.Load())

	terminating := loadDeployment(t, stores, deployment.Metadata.Name)
	require.NotNil(t, terminating.Metadata.DeletionTimestamp)
	require.Contains(t, loadDeploymentFinalizers(t, stores, deployment.Metadata.Name), DeploymentControllerFinalizer)
	purged, err := stores[v1alpha1.KindDeployment].PurgeFinalized(ctx)
	require.NoError(t, err)
	require.Zero(t, purged)

	adapter.removeErr = nil
	require.Eventually(t, func() bool {
		processed, err = controller.RunOnce(ctx)
		return err == nil && adapter.removeCalls.Load() == 2
	}, time.Second, 10*time.Millisecond)

	requireDeploymentMissing(t, stores, deployment.Metadata.Name)
}

func TestDeploymentController_DeleteAbandonsPendingApplyWork(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-with-apply-pending", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))
	_, err = controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.applyCalls.Load(), "pending apply work must not run after delete")
	require.Equal(t, int32(1), adapter.removeCalls.Load())
	requireDeploymentMissing(t, stores, deployment.Metadata.Name)
}

func TestDeploymentController_QueuedApplySeesCurrentDeleteState(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-with-apply-claimed", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.applyCalls.Load(), "queued apply must see the latest terminating row")
	require.Equal(t, int32(1), adapter.removeCalls.Load())
	requireDeploymentMissing(t, stores, deployment.Metadata.Name)
}

func TestDeploymentController_DeleteFinalizesWhenRuntimeRefMissing(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "delete-missing-runtime", v1alpha1.DesiredStateDeployed)
	require.NoError(t, stores[v1alpha1.KindRuntime].Delete(ctx, "default", "kubernetes-default", ""))
	require.NoError(t, stores[v1alpha1.KindDeployment].Delete(ctx, "default", deployment.Metadata.Name, ""))

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Zero(t, adapter.removeCalls.Load(), "missing runtime cannot dispatch adapter remove")
	requireDeploymentMissing(t, stores, deployment.Metadata.Name)
}

func TestDeploymentController_QueuedDeploymentUsesLatestGeneration(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	deployment := seedDeployment(t, stores, "stale", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	deployment.Spec.RuntimeConfig = map[string]any{"changed": true}
	_, err = stores[v1alpha1.KindDeployment].Upsert(ctx, deployment)
	require.NoError(t, err)

	latest := loadDeployment(t, stores, deployment.Metadata.Name)
	require.Greater(t, latest.Metadata.Generation, deployment.Metadata.Generation)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())
	require.Equal(t, latest.Metadata.Generation, adapter.lastApplyGeneration.Load())
}

// TestDeploymentController_ReappliesWhenReferencedSkillResolves regression-covers
// the sequence where deployment reconciles race ahead of skill resolution: the
// apply fails while the skill is unresolved, the Skill controller then lands a
// status-only resolvedSource patch, and that write must emit a control-plane
// event whose replay requeues the deployment so it succeeds.
func TestDeploymentController_ReappliesWhenReferencedSkillResolves(t *testing.T) {
	ctx := context.Background()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	seedSkill(t, stores, "docs")
	seedAgentWithSkill(t, stores, "assistant", "docs")
	seedAgentDeployment(t, stores, "assistant-deploy", "assistant", v1alpha1.DesiredStateDeployed)

	adapter := &recordingDeploymentAdapter{applyErr: errors.New("skill is not ready: no Ready=True resolved source")}
	controller := newDeploymentTestController(stores, adapter)
	controller.Events = v1alpha1store.NewControlPlaneEventStore(pool, v1alpha1store.TestSchema())

	_, err := controller.Refresh(ctx)
	require.NoError(t, err)
	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	adapter.applyErr = nil
	resolveSkill(t, stores, "docs", "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	res, err := controller.Drain(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, res.Events, "status-only resolvedSource write must emit a control-plane event")

	require.Eventually(t, func() bool {
		if _, err := controller.RunOnce(ctx); err != nil {
			return false
		}
		return adapter.applyCalls.Load() == 2
	}, time.Second, 10*time.Millisecond)

	got := loadDeployment(t, stores, "assistant-deploy")
	ready := got.Status.GetCondition("Ready")
	require.NotNil(t, ready)
	require.Equal(t, v1alpha1.ConditionTrue, ready.Status)
}

func TestDeploymentController_ResultFingerprinterPersistsDependencies(t *testing.T) {
	ctx := context.Background()
	stores := newControllerTestStores(t)
	seedMCPServer(t, stores, "weather")
	seedAgent(t, stores, "assistant", []v1alpha1.ResourceRef{{Name: "weather"}})
	seedAgentDeployment(t, stores, "assistant-deploy", "assistant", v1alpha1.DesiredStateDeployed)

	adapter := &resultFingerprintingDeploymentAdapter{}
	controller := newDeploymentTestController(stores, adapter)
	_, err := controller.FullReconcile(ctx)
	require.NoError(t, err)

	processed, err := controller.RunOnce(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, processed)
	require.Equal(t, int32(1), adapter.applyCalls.Load())

	applied := loadDeployment(t, stores, "assistant-deploy")
	var details deploymentControllerDetails
	ok, err := applied.Status.GetDetailsKey(deploymentControllerDetailsKey, &details)
	require.NoError(t, err)
	require.True(t, ok)
	require.NotEmpty(t, details.LastAppliedFingerprint)
	require.Len(t, details.Dependencies, 1)
	require.Equal(t, v1alpha1.KindMCPServer, details.Dependencies[0].Kind)
	require.Equal(t, "weather", details.Dependencies[0].Name)
	require.NotEmpty(t, details.Dependencies[0].MaterialHash)
}

func newControllerTestStores(t *testing.T) map[string]*v1alpha1store.Store {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	return v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
}

func newDeploymentTestController(
	stores map[string]*v1alpha1store.Store,
	adapter types.DeploymentAdapter,
) *DeploymentController {
	if adapter == nil {
		adapter = &recordingDeploymentAdapter{}
	}
	return &DeploymentController{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{v1alpha1.TypeKubernetes: adapter},
		Getter:   internaldb.NewGetter(stores),
	}
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
					Origin: v1alpha1.MCPPackageOrigin{
						Type:       v1alpha1.MCPPackageOriginTypeOCI,
						Identifier: identifier,
						OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: name},
					},
					Transport: v1alpha1.MCPTransport{Type: "stdio"},
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

func seedSkill(t *testing.T, stores map[string]*v1alpha1store.Store, name string) {
	t.Helper()
	_, err := stores[v1alpha1.KindSkill].Upsert(context.Background(), &v1alpha1.Skill{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.SkillSpec{
			Source: &v1alpha1.SkillSource{Repository: &v1alpha1.Repository{URL: "https://github.com/acme/" + name}},
		},
	})
	require.NoError(t, err)
}

// resolveSkill lands the same status-only patch the Skill controller writes
// when it pins a skill's source.
func resolveSkill(t *testing.T, stores map[string]*v1alpha1store.Store, name, commit string) {
	t.Helper()
	err := stores[v1alpha1.KindSkill].ApplyPatch(context.Background(), "default", name, v1alpha1store.DefaultTag(), v1alpha1store.PatchOpts{
		Status: func(current json.RawMessage) (json.RawMessage, error) {
			sk := &v1alpha1.Skill{}
			if err := sk.UnmarshalStatus(current); err != nil {
				return nil, err
			}
			sk.Status.ResolvedSource = &v1alpha1.SkillResolvedSource{Commit: commit}
			sk.Status.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "Resolved"})
			return sk.MarshalStatus()
		},
	})
	require.NoError(t, err)
}

func seedAgentWithSkill(t *testing.T, stores map[string]*v1alpha1store.Store, name, skillName string) {
	t.Helper()
	_, err := stores[v1alpha1.KindAgent].Upsert(context.Background(), &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
		Spec: v1alpha1.AgentSpec{
			Title:  "test agent",
			Source: &v1alpha1.AgentSource{Image: "ghcr.io/acme/" + name + ":1.0.0"},
			Skills: []v1alpha1.ResourceRef{{Name: skillName}},
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
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "kubernetes-default"},
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
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "kubernetes-default"},
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
	applyCalls          atomic.Int32
	removeCalls         atomic.Int32
	lastApplyGeneration atomic.Int64
	applyErr            error
	removeErr           error
}

func (a *recordingDeploymentAdapter) Type() string { return v1alpha1.TypeKubernetes }

func (a *recordingDeploymentAdapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindMCPServer, v1alpha1.KindAgent}
}

func (a *recordingDeploymentAdapter) Apply(_ context.Context, input types.ApplyInput) (*types.ApplyResult, error) {
	a.applyCalls.Add(1)
	if input.Deployment != nil {
		a.lastApplyGeneration.Store(input.Deployment.Metadata.Generation)
	}
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

// resultFingerprintingDeploymentAdapter implements the result-carrying
// fingerprint interface so its dependency snapshots must survive into the
// persisted controller details.
type resultFingerprintingDeploymentAdapter struct {
	recordingDeploymentAdapter
}

func (a *resultFingerprintingDeploymentAdapter) DesiredFingerprintResult(ctx context.Context, in types.ApplyInput) (types.ApplyFingerprintResult, error) {
	return types.DefaultApplyFingerprintResult(ctx, in, types.ApplyFingerprintOptions{AdapterType: a.Type()})
}
