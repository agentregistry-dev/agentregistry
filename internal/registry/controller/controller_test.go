package controller

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestDeploymentControllerSyncReplaysIgnoredEvents(t *testing.T) {
	reader := fakeEventReader{
		events: []v1alpha1store.ControlPlaneEvent{
			{Revision: 1, Key: v1alpha1store.ResourceKey{Kind: "Ignored", Namespace: "default", Name: "ignored"}, Operation: "insert"},
			{Revision: 2, Key: v1alpha1store.ResourceKey{Kind: "Other", Namespace: "default", Name: "other"}, Operation: "insert"},
		},
	}
	controller := &DeploymentController{Events: reader}

	res, err := controller.Sync(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), res.Checkpoint)
	require.Equal(t, 2, res.Events)
}

func TestDeploymentControllerHandleDependencyEventsFullReconcile(t *testing.T) {
	controller := &DeploymentController{}

	for _, kind := range []string{v1alpha1.KindPlugin, v1alpha1.KindSkill, v1alpha1.KindPrompt, v1alpha1.KindModel} {
		t.Run(kind, func(t *testing.T) {
			_, err := controller.HandleEvent(context.Background(), v1alpha1store.ControlPlaneEvent{
				Key: v1alpha1store.ResourceKey{Kind: kind, Namespace: "default", Name: "changed"},
			})
			require.ErrorContains(t, err, "no Deployment store registered")
		})
	}
}

func TestDeploymentControllerReplayDrainsMultipleBatches(t *testing.T) {
	reader := fakeEventReader{
		events: []v1alpha1store.ControlPlaneEvent{
			{Revision: 1, Key: v1alpha1store.ResourceKey{Kind: "Ignored"}},
			{Revision: 2, Key: v1alpha1store.ResourceKey{Kind: "Ignored"}},
			{Revision: 3, Key: v1alpha1store.ResourceKey{Kind: "Ignored"}},
			{Revision: 4, Key: v1alpha1store.ResourceKey{Kind: "Ignored"}},
			{Revision: 5, Key: v1alpha1store.ResourceKey{Kind: "Ignored"}},
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

// TestDeploymentControllerRunSurvivesTransientDrainFailure covers the requeue
// gap where a transient replay error killed the Run loop: exiting shuts the
// workqueue down, silently dropping already-scheduled rate-limited retries and
// the resync ticker, so a Deployment that failed on a not-yet-ready dependency
// was never reconciled again. Run must ride out the failure and drain the next
// wakeup normally.
func TestDeploymentControllerRunSurvivesTransientDrainFailure(t *testing.T) {
	reader := &flakyEventReader{}
	wakeups := make(chan struct{}, 1)
	controller := &DeploymentController{
		Stores: map[string]*v1alpha1store.Store{
			v1alpha1.KindDeployment: v1alpha1store.NewStore(nil, pkgdb.MustNewSchema(pkgdb.OSSSchema), "deployments"),
		},
		Adapters: map[string]types.DeploymentAdapter{"Stub": stubDeploymentAdapter{}},
		Getter: func(context.Context, v1alpha1.ResourceRef) (v1alpha1.Object, error) {
			return nil, pkgdb.ErrNotFound
		},
		Events:  reader,
		Wakeups: wakeups,
	}
	controller.markReady(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runErr := make(chan error, 1)
	go func() { runErr <- controller.Run(ctx, 0) }()

	reader.set(true, nil)
	wakeups <- struct{}{}
	require.Eventually(t, func() bool { return !controller.Ready() }, time.Second, time.Millisecond,
		"failed drain should mark the controller not ready")

	reader.set(false, []v1alpha1store.ControlPlaneEvent{{
		Revision: 1,
		Key:      v1alpha1store.ResourceKey{Kind: "Ignored", Namespace: "default", Name: "x"},
	}})
	wakeups <- struct{}{}
	require.Eventually(t, func() bool { return controller.Ready() && controller.Checkpoint() == 1 },
		time.Second, time.Millisecond, "the loop must survive the failed drain and process the next wakeup")

	select {
	case err := <-runErr:
		t.Fatalf("Run exited on a transient drain failure: %v", err)
	default:
	}

	cancel()
	select {
	case err := <-runErr:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}
}

type stubDeploymentAdapter struct{}

func (stubDeploymentAdapter) Type() string { return "Stub" }

func (stubDeploymentAdapter) SupportedTargetKinds() []string { return []string{v1alpha1.KindAgent} }

func (stubDeploymentAdapter) Apply(context.Context, types.ApplyInput) (*types.ApplyResult, error) {
	return nil, nil
}

func (stubDeploymentAdapter) Remove(context.Context, types.RemoveInput) (*types.RemoveResult, error) {
	return nil, nil
}

func (stubDeploymentAdapter) Logs(context.Context, types.LogsInput) (<-chan types.LogLine, error) {
	return nil, nil
}

// flakyEventReader is a concurrency-safe ControlPlaneEventReader whose failure
// mode can be toggled mid-test.
type flakyEventReader struct {
	mu     sync.Mutex
	fail   bool
	events []v1alpha1store.ControlPlaneEvent
}

func (f *flakyEventReader) set(fail bool, events []v1alpha1store.ControlPlaneEvent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = fail
	f.events = events
}

func (f *flakyEventReader) ListAfter(_ context.Context, afterRevision int64, limit int) ([]v1alpha1store.ControlPlaneEvent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return nil, errors.New("transient database error")
	}
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

func (f *flakyEventReader) OldestRevision(context.Context) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, false, errors.New("transient database error")
	}
	if len(f.events) == 0 {
		return 0, false, nil
	}
	return f.events[0].Revision, true, nil
}

func (f *flakyEventReader) CurrentRevision(context.Context) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return 0, errors.New("transient database error")
	}
	if len(f.events) == 0 {
		return 0, nil
	}
	return f.events[len(f.events)-1].Revision, nil
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
