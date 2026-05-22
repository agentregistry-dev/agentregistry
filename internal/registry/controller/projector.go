package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"istio.io/istio/pkg/kube/krt"
)

// ErrProjectorNotReady is returned until Refresh completes successfully.
var ErrProjectorNotReady = errors.New("controller projector is not ready")

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
	Wakeups    <-chan struct{}

	mu         sync.RWMutex
	checkpoint int64
	ready      bool
	lastErr    error
}

// SyncResult describes one projector replay pass.
type SyncResult struct {
	Checkpoint   int64
	Events       int
	FullResynced bool
}

// Ready reports whether the projector has completed an initial refresh and is
// serving a complete read model.
func (p *Projector) Ready() bool {
	if p == nil {
		return false
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.ready
}

// Checkpoint returns the last fully applied event revision.
func (p *Projector) Checkpoint() int64 {
	if p == nil {
		return 0
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.checkpoint
}

// ReadinessError explains why callers should not trust the current read model.
func (p *Projector) ReadinessError() error {
	if p == nil {
		return ErrProjectorNotReady
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.ready {
		return nil
	}
	if p.lastErr != nil {
		return p.lastErr
	}
	return ErrProjectorNotReady
}

// Refresh performs a full repair pass. It captures the durable event
// high-water mark before rebuilding canonical state, then replays anything
// newer so writes racing the refresh are not skipped.
func (p *Projector) Refresh(ctx context.Context) (SyncResult, error) {
	if err := p.validate(); err != nil {
		p.markNotReady(err)
		return SyncResult{}, err
	}
	result, err := p.fullRefreshAndReplay(ctx)
	if err != nil {
		p.markNotReady(err)
		return SyncResult{}, err
	}
	p.markReady(result.Checkpoint)
	return result, nil
}

// Drain replays retained events after the internal checkpoint. If pruning
// created a gap, it falls back to Refresh so the model is rebuilt from
// canonical tables instead of a partial retained tail.
func (p *Projector) Drain(ctx context.Context) (SyncResult, error) {
	if err := p.validate(); err != nil {
		p.markNotReady(err)
		return SyncResult{}, err
	}
	start := p.Checkpoint()
	result, err := p.Sync(ctx, start)
	if err != nil {
		p.markNotReady(err)
		return SyncResult{}, err
	}
	p.markReady(result.Checkpoint)
	return result, nil
}

// Run keeps the projector repaired. Wakeups should be wired to coarse database
// invalidations; the resync ticker is a periodic safety refresh.
func (p *Projector) Run(ctx context.Context, resyncInterval time.Duration) error {
	if p == nil {
		return errors.New("controller projector: projector is required")
	}
	if !p.Ready() {
		if _, err := p.Refresh(ctx); err != nil {
			return err
		}
	}
	var ticker *time.Ticker
	var ticks <-chan time.Time
	if resyncInterval > 0 {
		ticker = time.NewTicker(resyncInterval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.Wakeups:
			if _, err := p.Drain(ctx); err != nil {
				return err
			}
		case <-ticks:
			if _, err := p.Refresh(ctx); err != nil {
				return err
			}
		}
	}
}

// Sync replays retained events after checkpoint. If pruning created a gap, it
// calls FullResync and advances only after replaying newer events.
func (p *Projector) Sync(ctx context.Context, checkpoint int64) (SyncResult, error) {
	if err := p.validate(); err != nil {
		return SyncResult{}, err
	}
	oldest, ok, err := p.Events.OldestRevision(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	if ok && ((checkpoint == 0 && oldest > 1) || (checkpoint > 0 && checkpoint < oldest-1)) {
		return p.fullRefreshAndReplay(ctx)
	}

	return p.replay(ctx, checkpoint)
}

func (p *Projector) validate() error {
	if p == nil || p.Events == nil {
		return fmt.Errorf("controller projector: event reader is required")
	}
	return nil
}

func (p *Projector) fullRefreshAndReplay(ctx context.Context) (SyncResult, error) {
	if p.FullResync == nil {
		oldest, _, _ := p.Events.OldestRevision(ctx)
		return SyncResult{}, fmt.Errorf("controller projector: retained events do not cover checkpoint; oldest revision is %d", oldest)
	}
	highWater, err := p.Events.CurrentRevision(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	if err := p.FullResync(ctx); err != nil {
		return SyncResult{}, fmt.Errorf("controller projector full resync: %w", err)
	}
	replayed, err := p.replay(ctx, highWater)
	if err != nil {
		return SyncResult{}, err
	}
	replayed.FullResynced = true
	return replayed, nil
}

func (p *Projector) replay(ctx context.Context, checkpoint int64) (SyncResult, error) {
	limit := p.BatchLimit
	if limit <= 0 {
		limit = 500
	}
	next := checkpoint
	applied := 0
	for {
		events, err := p.Events.ListAfter(ctx, next, limit)
		if err != nil {
			return SyncResult{}, err
		}
		if len(events) == 0 {
			return SyncResult{Checkpoint: next, Events: applied}, nil
		}
		for _, event := range events {
			if p.ApplyEvent != nil {
				if err := p.ApplyEvent(ctx, event); err != nil {
					return SyncResult{}, fmt.Errorf("controller projector apply revision %d: %w", event.Revision, err)
				}
			}
			next = event.Revision
			applied++
		}
	}
}

func (p *Projector) markReady(checkpoint int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.checkpoint = checkpoint
	p.ready = true
	p.lastErr = nil
}

func (p *Projector) markNotReady(err error) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ready = false
	p.lastErr = err
}

// SourceObject is the KRT-facing row projected from the durable control-plane
// event stream. Projectors keep these collections current; translators derive
// desired work from them.
type SourceObject struct {
	Key   v1alpha1store.ResourceKey
	Event v1alpha1store.ControlPlaneEvent
}

// ResourceName implements krt.ResourceNamer.
func (s SourceObject) ResourceName() string {
	return sourceObjectKey(s.Key)
}

// SourceCollection is a KRT StaticCollection-backed source read model.
type SourceCollection struct {
	items krt.StaticCollection[SourceObject]
}

// NewSourceCollection constructs an empty source collection.
func NewSourceCollection() *SourceCollection {
	return &SourceCollection{
		items: krt.NewStaticCollection[SourceObject](nil, nil, krt.WithName("agentregistry-source")),
	}
}

// Apply records the latest event for a source key, or removes it on delete.
func (c *SourceCollection) Apply(event v1alpha1store.ControlPlaneEvent) {
	if c == nil {
		return
	}
	if event.Operation == "delete" {
		c.items.DeleteObject(sourceObjectKey(event.Key))
		return
	}
	c.items.UpdateObject(SourceObject{Key: event.Key, Event: event})
}

// Get returns the last applied event for key.
func (c *SourceCollection) Get(key v1alpha1store.ResourceKey) (v1alpha1store.ControlPlaneEvent, bool) {
	if c == nil {
		return v1alpha1store.ControlPlaneEvent{}, false
	}
	item := c.items.GetKey(sourceObjectKey(key))
	if item == nil {
		return v1alpha1store.ControlPlaneEvent{}, false
	}
	return item.Event, true
}

// Collection exposes the KRT collection for derivation code.
func (c *SourceCollection) Collection() krt.Collection[SourceObject] {
	if c == nil {
		return nil
	}
	return c.items
}

func sourceObjectKey(key v1alpha1store.ResourceKey) string {
	return fmt.Sprintf("%s/%s/%s/%s", key.Kind, key.Namespace, key.Name, key.Tag)
}
