package controller

import (
	"context"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// ControlPlaneEventReader is the event-log surface a projector needs. The
// concrete implementation is v1alpha1store.ControlPlaneEventStore; tests can
// supply fakes without a database.
type ControlPlaneEventReader interface {
	ListAfter(ctx context.Context, afterRevision int64, limit int) ([]v1alpha1store.ControlPlaneEvent, error)
	OldestRevision(ctx context.Context) (revision int64, ok bool, err error)
	CurrentRevision(ctx context.Context) (int64, error)
}

// FullResyncFunc reloads canonical source tables when the event log no longer
// covers the projector checkpoint.
type FullResyncFunc func(ctx context.Context) error

// EventApplyFunc updates process-local source collections for one invalidation
// event. The implementation should re-read the canonical row named by event.Key.
type EventApplyFunc func(ctx context.Context, event v1alpha1store.ControlPlaneEvent) error

// Projector is the behavior-preserving replay skeleton for KRT source
// collections. It projects durable database invalidations into a controller
// read model; translators/derivers later turn that read model into desired
// work. It owns no adapter side effects and only advances a checkpoint.
type Projector struct {
	Events     ControlPlaneEventReader
	FullResync FullResyncFunc
	ApplyEvent EventApplyFunc
	BatchLimit int
}

// SyncResult describes one projector replay pass.
type SyncResult struct {
	Checkpoint   int64
	Events       int
	FullResynced bool
}

// Sync replays retained events after checkpoint. If pruning created a gap, it
// calls FullResync and advances to the current high-water revision.
func (p *Projector) Sync(ctx context.Context, checkpoint int64) (SyncResult, error) {
	if p == nil || p.Events == nil {
		return SyncResult{}, fmt.Errorf("controller projector: event reader is required")
	}
	oldest, ok, err := p.Events.OldestRevision(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	if ok && checkpoint > 0 && checkpoint < oldest-1 {
		if p.FullResync == nil {
			return SyncResult{}, fmt.Errorf("controller projector: checkpoint %d is older than retained event range starting at %d", checkpoint, oldest)
		}
		if err := p.FullResync(ctx); err != nil {
			return SyncResult{}, fmt.Errorf("controller projector full resync: %w", err)
		}
		current, err := p.Events.CurrentRevision(ctx)
		if err != nil {
			return SyncResult{}, err
		}
		return SyncResult{Checkpoint: current, FullResynced: true}, nil
	}

	limit := p.BatchLimit
	if limit <= 0 {
		limit = 500
	}
	events, err := p.Events.ListAfter(ctx, checkpoint, limit)
	if err != nil {
		return SyncResult{}, err
	}
	next := checkpoint
	for _, event := range events {
		if p.ApplyEvent != nil {
			if err := p.ApplyEvent(ctx, event); err != nil {
				return SyncResult{}, fmt.Errorf("controller projector apply revision %d: %w", event.Revision, err)
			}
		}
		next = event.Revision
	}
	return SyncResult{Checkpoint: next, Events: len(events)}, nil
}

// SourceCollection is a small process-local collection skeleton used by tests
// and early controller code before a concrete KRT collection is wired in.
type SourceCollection struct {
	items map[v1alpha1store.ResourceKey]v1alpha1store.ControlPlaneEvent
}

// NewSourceCollection constructs an empty source collection.
func NewSourceCollection() *SourceCollection {
	return &SourceCollection{items: map[v1alpha1store.ResourceKey]v1alpha1store.ControlPlaneEvent{}}
}

// Apply records the latest event for a source key, or removes it on delete.
func (c *SourceCollection) Apply(event v1alpha1store.ControlPlaneEvent) {
	if c.items == nil {
		c.items = map[v1alpha1store.ResourceKey]v1alpha1store.ControlPlaneEvent{}
	}
	if event.Operation == "delete" {
		delete(c.items, event.Key)
		return
	}
	c.items[event.Key] = event
}

// Get returns the last applied event for key.
func (c *SourceCollection) Get(key v1alpha1store.ResourceKey) (v1alpha1store.ControlPlaneEvent, bool) {
	if c == nil || c.items == nil {
		return v1alpha1store.ControlPlaneEvent{}, false
	}
	event, ok := c.items[key]
	return event, ok
}
