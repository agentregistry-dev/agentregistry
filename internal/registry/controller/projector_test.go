package controller

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestProjectorSyncReplaysEventsIntoSourceCollection(t *testing.T) {
	reader := fakeEventReader{
		events: []v1alpha1store.ControlPlaneEvent{
			{Revision: 1, Key: v1alpha1store.ResourceKey{Kind: "Deployment", Namespace: "default", Name: "api"}, Operation: "insert"},
			{Revision: 2, Key: v1alpha1store.ResourceKey{Kind: "Deployment", Namespace: "default", Name: "worker"}, Operation: "insert"},
		},
	}
	collection := NewSourceCollection()
	projector := &Projector{
		Events: reader,
		ApplyEvent: func(_ context.Context, event v1alpha1store.ControlPlaneEvent) error {
			collection.Apply(event)
			return nil
		},
	}

	res, err := projector.Sync(context.Background(), 0)
	require.NoError(t, err)
	require.Equal(t, int64(2), res.Checkpoint)
	require.Equal(t, 2, res.Events)

	_, ok := collection.Get(v1alpha1store.ResourceKey{Kind: "Deployment", Namespace: "default", Name: "api"})
	require.True(t, ok)
	_, ok = collection.Get(v1alpha1store.ResourceKey{Kind: "Deployment", Namespace: "default", Name: "worker"})
	require.True(t, ok)
}

func TestProjectorSyncFullResyncsWhenCheckpointFallsBehindRetention(t *testing.T) {
	reader := fakeEventReader{
		oldest:  10,
		current: 15,
		events:  []v1alpha1store.ControlPlaneEvent{{Revision: 10}},
	}
	fullResyncs := 0
	projector := &Projector{
		Events: reader,
		FullResync: func(context.Context) error {
			fullResyncs++
			return nil
		},
	}

	res, err := projector.Sync(context.Background(), 5)
	require.NoError(t, err)
	require.True(t, res.FullResynced)
	require.Equal(t, int64(15), res.Checkpoint)
	require.Equal(t, 1, fullResyncs)
}

func TestSourceCollectionDeletesOnDeleteEvent(t *testing.T) {
	key := v1alpha1store.ResourceKey{Kind: "Deployment", Namespace: "default", Name: "api"}
	collection := NewSourceCollection()

	collection.Apply(v1alpha1store.ControlPlaneEvent{Revision: 1, Key: key, Operation: "insert"})
	_, ok := collection.Get(key)
	require.True(t, ok)

	collection.Apply(v1alpha1store.ControlPlaneEvent{Revision: 2, Key: key, Operation: "delete"})
	_, ok = collection.Get(key)
	require.False(t, ok)
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
