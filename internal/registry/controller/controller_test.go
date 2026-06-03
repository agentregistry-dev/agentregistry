package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestDeploymentControllerSyncReplaysIgnoredEvents(t *testing.T) {
	reader := fakeEventReader{
		events: []v1alpha1store.ControlPlaneEvent{
			{Revision: 1, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindSkill, Namespace: "default", Name: "skill"}, Operation: "insert"},
			{Revision: 2, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindPrompt, Namespace: "default", Name: "prompt"}, Operation: "insert"},
		},
	}
	controller := &DeploymentController{Events: reader}

	res, err := controller.Sync(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), res.Checkpoint)
	require.Equal(t, 2, res.Events)
}

func TestDeploymentControllerReplayDrainsMultipleBatches(t *testing.T) {
	reader := fakeEventReader{
		events: []v1alpha1store.ControlPlaneEvent{
			{Revision: 1, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindPrompt}},
			{Revision: 2, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindPrompt}},
			{Revision: 3, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindPrompt}},
			{Revision: 4, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindPrompt}},
			{Revision: 5, Key: v1alpha1store.ResourceKey{Kind: v1alpha1.KindPrompt}},
		},
	}
	controller := &DeploymentController{Events: reader, BatchLimit: 2}

	res, err := controller.Sync(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, int64(5), res.Checkpoint)
	require.Equal(t, 5, res.Events)
}

func TestDeploymentControllerFailedHandleDoesNotAdvanceCheckpoint(t *testing.T) {
	reader := fakeEventReader{
		events: []v1alpha1store.ControlPlaneEvent{{
			Revision: 1,
			Key:      v1alpha1store.ResourceKey{Kind: v1alpha1.KindDeployment, Namespace: "default", Name: "api"},
		}},
	}
	controller := &DeploymentController{Events: reader}

	_, err := controller.Drain(context.Background())
	require.ErrorContains(t, err, "no Deployment store registered")
	require.False(t, controller.Ready())
	require.Equal(t, int64(0), controller.Checkpoint())
}

func TestDeploymentControllerNotReadyBeforeInitialRefresh(t *testing.T) {
	controller := &DeploymentController{}
	require.False(t, controller.Ready())
	require.ErrorIs(t, controller.ReadinessError(), ErrControllerNotReady)

	_, err := controller.Refresh(context.Background())
	require.ErrorContains(t, err, "event reader is required")
	require.False(t, controller.Ready())
	require.ErrorContains(t, controller.ReadinessError(), "event reader is required")
}

type fakeEventReader struct {
	events  []v1alpha1store.ControlPlaneEvent
	oldest  int64
	current int64
}

func (f fakeEventReader) ListAfter(_ context.Context, afterRevision int64, limit int) ([]v1alpha1store.ControlPlaneEvent, error) {
	var out []v1alpha1store.ControlPlaneEvent
	for _, event := range f.events {
		if event.Revision > afterRevision {
			out = append(out, event)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (f fakeEventReader) OldestRevision(context.Context) (int64, bool, error) {
	if f.oldest > 0 {
		return f.oldest, true, nil
	}
	if len(f.events) == 0 {
		return 0, false, nil
	}
	return f.events[0].Revision, true, nil
}

func (f fakeEventReader) CurrentRevision(context.Context) (int64, error) {
	if f.current > 0 {
		return f.current, nil
	}
	if len(f.events) == 0 {
		return 0, nil
	}
	return f.events[len(f.events)-1].Revision, nil
}
