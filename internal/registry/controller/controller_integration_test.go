//go:build integration

package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestDeploymentControllerFullReconcileSchedulesDeployments(t *testing.T) {
	ctx := context.Background()
	stores, workStore, _ := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	seedDeployment(t, stores, "api", v1alpha1.DesiredStateDeployed)
	seedDeployment(t, stores, "worker", v1alpha1.DesiredStateDeployed)
	controller := newDeploymentTestController(stores, workStore)

	count, err := controller.FullReconcile(ctx)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	claimed, err := workStore.ClaimDue(ctx, "test", controller.now().Add(defaultExecutorLeaseDuration), 10)
	require.NoError(t, err)
	require.Len(t, claimed, 2)
}

func TestDeploymentControllerHandleDeploymentEventSchedulesOneDeployment(t *testing.T) {
	ctx := context.Background()
	stores, workStore, _ := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	target := seedDeployment(t, stores, "api", v1alpha1.DesiredStateDeployed)
	seedDeployment(t, stores, "worker", v1alpha1.DesiredStateDeployed)
	controller := newDeploymentTestController(stores, workStore)

	count, err := controller.HandleEvent(ctx, v1alpha1store.ControlPlaneEvent{
		Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: target.Metadata.Name},
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)

	claimed, err := workStore.ClaimDue(ctx, "test", controller.now().Add(defaultExecutorLeaseDuration), 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
	require.Equal(t, "api", claimed[0].Resource.Name)
}

func TestDeploymentControllerHandleMissingDeploymentEventNoops(t *testing.T) {
	ctx := context.Background()
	stores, workStore, _ := newControllerTestStores(t)
	controller := newDeploymentTestController(stores, workStore)

	count, err := controller.HandleEvent(ctx, v1alpha1store.ControlPlaneEvent{
		Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: "missing"},
	})
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestDeploymentControllerHandleDependencyEventsFullReconcileDeployments(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{v1alpha1.KindRuntime, v1alpha1.KindAgent, v1alpha1.KindMCPServer} {
		t.Run(kind, func(t *testing.T) {
			stores, workStore, _ := newControllerTestStores(t)
			seedRuntime(t, stores, "local")
			seedMCPServer(t, stores, "weather")
			seedDeployment(t, stores, "api", v1alpha1.DesiredStateDeployed)
			seedDeployment(t, stores, "worker", v1alpha1.DesiredStateDeployed)
			controller := newDeploymentTestController(stores, workStore)

			count, err := controller.HandleEvent(ctx, v1alpha1store.ControlPlaneEvent{
				Key: v1alpha1store.ResourceKey{Kind: kind, Namespace: "default", Name: "changed"},
			})
			require.NoError(t, err)
			require.Equal(t, 2, count)
		})
	}
}

func TestDeploymentControllerHandleSkillPromptEventsNoop(t *testing.T) {
	ctx := context.Background()
	stores, workStore, _ := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	seedDeployment(t, stores, "api", v1alpha1.DesiredStateDeployed)
	controller := newDeploymentTestController(stores, workStore)

	for _, kind := range []string{v1alpha1.KindSkill, v1alpha1.KindPrompt} {
		count, err := controller.HandleEvent(ctx, v1alpha1store.ControlPlaneEvent{
			Key: v1alpha1store.ResourceKey{Kind: kind, Namespace: "default", Name: "changed"},
		})
		require.NoError(t, err)
		require.Zero(t, count)
	}
}

func TestDeploymentControllerRetentionGapTriggersFullReconcile(t *testing.T) {
	ctx := context.Background()
	stores, workStore, _ := newControllerTestStores(t)
	seedRuntime(t, stores, "local")
	seedMCPServer(t, stores, "weather")
	seedDeployment(t, stores, "api", v1alpha1.DesiredStateDeployed)
	controller := newDeploymentTestController(stores, workStore)
	controller.Events = fakeEventReader{
		oldest:  10,
		current: 12,
		events:  []v1alpha1store.ControlPlaneEvent{{Revision: 13, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindPrompt}}},
	}

	result, err := controller.Sync(ctx, 5)
	require.NoError(t, err)
	require.True(t, result.FullResynced)
	require.Equal(t, int64(13), result.Checkpoint)

	claimed, err := workStore.ClaimDue(ctx, "test", controller.now().Add(defaultExecutorLeaseDuration), 10)
	require.NoError(t, err)
	require.Len(t, claimed, 1)
}
