package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"k8s.io/client-go/util/workqueue"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	defaultControllerEventBatchLimit = 500
	defaultControllerListPageSize    = 500
)

// ErrControllerNotReady is returned until Refresh completes successfully.
var ErrControllerNotReady = errors.New("deployment controller is not ready")

// ControlPlaneEventReader is the durable event-log surface the controller uses
// to replay source invalidations.
type ControlPlaneEventReader interface {
	ListAfter(ctx context.Context, afterRevision int64, limit int) ([]v1alpha1store.ControlPlaneEvent, error)
	OldestRevision(ctx context.Context) (revision int64, ok bool, err error)
	CurrentRevision(ctx context.Context) (int64, error)
}

// DeploymentController replays durable source invalidations and reconciles
// Deployments through an in-memory workqueue. Source state remains durable in
// the v1alpha1 tables and control_plane_events; queued work is intentionally
// process-local and rebuilt by startup/repair full reconciles.
type DeploymentController struct {
	Stores   map[string]*v1alpha1store.Store
	Adapters map[string]types.DeploymentAdapter
	Getter   v1alpha1.GetterFunc
	Events   ControlPlaneEventReader

	BatchLimit int
	Wakeups    <-chan struct{}
	Queue      workqueue.TypedRateLimitingInterface[deploymentQueueKey]

	mu         sync.RWMutex
	checkpoint int64
	ready      bool
	lastErr    error

	queueMu sync.Mutex
}

// SyncResult describes one controller replay pass.
type SyncResult struct {
	Checkpoint   int64
	Events       int
	FullResynced bool
}

// Ready reports whether the controller has completed an initial refresh.
func (c *DeploymentController) Ready() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.ready
}

// Checkpoint returns the last fully handled event revision.
func (c *DeploymentController) Checkpoint() int64 {
	if c == nil {
		return 0
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.checkpoint
}

// ReadinessError explains why callers should not trust the controller state.
func (c *DeploymentController) ReadinessError() error {
	if c == nil {
		return ErrControllerNotReady
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.ready {
		return nil
	}
	if c.lastErr != nil {
		return c.lastErr
	}
	return ErrControllerNotReady
}

// FullReconcile schedules work for every current Deployment, including
// terminating rows that still need finalizer-driven teardown.
func (c *DeploymentController) FullReconcile(ctx context.Context) (int, error) {
	deployments, err := c.listDeployments(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, deployment := range deployments {
		if v1alpha1.IsDiscoveredDeployment(deployment) {
			continue
		}
		if err := c.enqueueDeployment(deployment); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// HandleEvent maps a source invalidation to Deployment work. Dependency
// changes intentionally use a full Deployment scan for this first controller
// foundation; Skill and Prompt events are retained for future dependency-aware
// controllers but do not affect Deployment reconciliation today.
func (c *DeploymentController) HandleEvent(ctx context.Context, event v1alpha1store.ControlPlaneEvent) (int, error) {
	switch event.Key.Kind {
	case v1alpha1.KindDeployment:
		return c.reconcileDeployment(ctx, event.Key)
	case v1alpha1.KindRuntime, v1alpha1.KindAgent, v1alpha1.KindMCPServer:
		return c.FullReconcile(ctx)
	case v1alpha1.KindSkill, v1alpha1.KindPrompt:
		return 0, nil
	default:
		return 0, nil
	}
}

// Refresh performs a full repair pass. It captures the durable event high-water
// mark before rebuilding Deployment work, then replays anything newer so writes
// racing the refresh are not skipped.
func (c *DeploymentController) Refresh(ctx context.Context) (SyncResult, error) {
	if err := c.validateReplay(); err != nil {
		c.markNotReady(err)
		return SyncResult{}, err
	}
	result, err := c.fullRefreshAndReplay(ctx)
	if err != nil {
		c.markNotReady(err)
		return SyncResult{}, err
	}
	c.markReady(result.Checkpoint)
	return result, nil
}

// Drain replays retained events after the internal checkpoint. If pruning
// created a gap, it falls back to Refresh.
func (c *DeploymentController) Drain(ctx context.Context) (SyncResult, error) {
	if err := c.validateReplay(); err != nil {
		c.markNotReady(err)
		return SyncResult{}, err
	}
	result, err := c.Sync(ctx, c.Checkpoint())
	if err != nil {
		c.markNotReady(err)
		return SyncResult{}, err
	}
	c.markReady(result.Checkpoint)
	return result, nil
}

// Run keeps Deployment reconciliation repaired. Wakeups should be wired to
// coarse database invalidations; the resync ticker is a periodic safety
// refresh. Adapter side effects run through the in-memory workqueue worker.
func (c *DeploymentController) Run(ctx context.Context, resyncInterval time.Duration) error {
	if c == nil {
		return errors.New("deployment controller: controller is required")
	}
	if !c.Ready() {
		if _, err := c.Refresh(ctx); err != nil {
			return err
		}
	}
	queue := c.workQueue()
	defer queue.ShutDown()

	workerErrs := make(chan error, 1)
	go func() {
		workerErrs <- c.RunWorker(ctx)
	}()

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
		case err := <-workerErrs:
			return err
		case <-c.Wakeups:
			if _, err := c.Drain(ctx); err != nil {
				return err
			}
		case <-ticks:
			if _, err := c.Refresh(ctx); err != nil {
				return err
			}
		}
	}
}

// RunWorker processes queued Deployment keys until the queue is shut down.
func (c *DeploymentController) RunWorker(ctx context.Context) error {
	if err := c.validateReconciler(); err != nil {
		return err
	}
	queue := c.workQueue()
	for {
		key, shutdown := queue.Get()
		if shutdown {
			return nil
		}
		c.processQueueItem(ctx, queue, key)
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
}

// RunOnce processes at most one currently ready queued Deployment key. It is
// primarily a test hook; RunWorker owns the blocking production loop.
func (c *DeploymentController) RunOnce(ctx context.Context) (int, error) {
	if err := c.validateReconciler(); err != nil {
		return 0, err
	}
	queue := c.workQueue()
	if queue.Len() == 0 {
		return 0, nil
	}
	key, shutdown := queue.Get()
	if shutdown {
		return 0, nil
	}
	c.processQueueItem(ctx, queue, key)
	return 1, nil
}

// Sync replays retained events after checkpoint. If pruning created a gap, it
// performs a full refresh and then replays newer events.
func (c *DeploymentController) Sync(ctx context.Context, checkpoint int64) (SyncResult, error) {
	if err := c.validateReplay(); err != nil {
		return SyncResult{}, err
	}
	oldest, ok, err := c.Events.OldestRevision(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	if ok && ((checkpoint == 0 && oldest > 1) || (checkpoint > 0 && checkpoint < oldest-1)) {
		return c.fullRefreshAndReplay(ctx)
	}
	return c.replay(ctx, checkpoint)
}

func (c *DeploymentController) reconcileDeployment(ctx context.Context, key v1alpha1store.ResourceKey) (int, error) {
	store := c.deploymentStore()
	if store == nil {
		return 0, errors.New("deployment controller: no Deployment store registered")
	}
	namespace := key.Namespace
	if namespace == "" {
		namespace = v1alpha1.DefaultNamespace
	}
	raw, err := store.GetLatestIncludingTerminating(ctx, namespace, key.Name)
	if err != nil {
		if errors.Is(err, pkgdb.ErrNotFound) {
			return 0, nil
		}
		return 0, fmt.Errorf("deployment controller: load Deployment %s/%s: %w", namespace, key.Name, err)
	}
	deployment, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Deployment {
		return &v1alpha1.Deployment{}
	}, raw, v1alpha1.KindDeployment)
	if err != nil {
		return 0, fmt.Errorf("deployment controller: decode Deployment %s/%s: %w", namespace, key.Name, err)
	}
	if v1alpha1.IsDiscoveredDeployment(deployment) {
		return 0, nil
	}
	if err := c.enqueueDeployment(deployment); err != nil {
		return 0, err
	}
	return 1, nil
}

func (c *DeploymentController) listDeployments(ctx context.Context) ([]*v1alpha1.Deployment, error) {
	store := c.deploymentStore()
	if store == nil {
		return nil, errors.New("deployment controller: no Deployment store registered")
	}
	var out []*v1alpha1.Deployment
	opts := v1alpha1store.ListOpts{Limit: defaultControllerListPageSize, IncludeTerminating: true}
	for {
		rows, cursor, err := store.List(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("deployment controller: list Deployments: %w", err)
		}
		for _, raw := range rows {
			deployment, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Deployment {
				return &v1alpha1.Deployment{}
			}, raw, v1alpha1.KindDeployment)
			if err != nil {
				return nil, fmt.Errorf("deployment controller: decode Deployment: %w", err)
			}
			out = append(out, deployment)
		}
		if cursor == "" {
			return out, nil
		}
		opts.Cursor = cursor
	}
}

func (c *DeploymentController) enqueueDeployment(deployment *v1alpha1.Deployment) error {
	if deployment == nil {
		return errors.New("deployment controller: deployment is required")
	}
	meta := deployment.Metadata
	if meta.Name == "" {
		return errors.New("deployment controller: deployment metadata.name is required")
	}
	c.workQueue().Add(deploymentQueueKey{
		Namespace: meta.NamespaceOrDefault(),
		Name:      meta.Name,
	})
	return nil
}

func (c *DeploymentController) fullRefreshAndReplay(ctx context.Context) (SyncResult, error) {
	highWater, err := c.Events.CurrentRevision(ctx)
	if err != nil {
		return SyncResult{}, err
	}
	if _, err := c.FullReconcile(ctx); err != nil {
		return SyncResult{}, fmt.Errorf("deployment controller full reconcile: %w", err)
	}
	replayed, err := c.replay(ctx, highWater)
	if err != nil {
		return SyncResult{}, err
	}
	replayed.FullResynced = true
	return replayed, nil
}

func (c *DeploymentController) replay(ctx context.Context, checkpoint int64) (SyncResult, error) {
	limit := c.BatchLimit
	if limit <= 0 {
		limit = defaultControllerEventBatchLimit
	}
	next := checkpoint
	applied := 0
	for {
		events, err := c.Events.ListAfter(ctx, next, limit)
		if err != nil {
			return SyncResult{}, err
		}
		if len(events) == 0 {
			return SyncResult{Checkpoint: next, Events: applied}, nil
		}
		for _, event := range events {
			if _, err := c.HandleEvent(ctx, event); err != nil {
				return SyncResult{}, fmt.Errorf("deployment controller handle revision %d: %w", event.Revision, err)
			}
			next = event.Revision
			applied++
		}
	}
}

func (c *DeploymentController) validateReplay() error {
	if c == nil || c.Events == nil {
		return errors.New("deployment controller: event reader is required")
	}
	return nil
}

func (c *DeploymentController) deploymentStore() *v1alpha1store.Store {
	if c == nil || c.Stores == nil {
		return nil
	}
	return c.Stores[v1alpha1.KindDeployment]
}

func (c *DeploymentController) workQueue() workqueue.TypedRateLimitingInterface[deploymentQueueKey] {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	if c.Queue == nil {
		c.Queue = workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[deploymentQueueKey](),
			workqueue.TypedRateLimitingQueueConfig[deploymentQueueKey]{Name: "deployment-controller"},
		)
	}
	return c.Queue
}

func (c *DeploymentController) markReady(checkpoint int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkpoint = checkpoint
	c.ready = true
	c.lastErr = nil
}

func (c *DeploymentController) markNotReady(err error) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ready = false
	c.lastErr = err
}

func refNamespace(refNamespace, fallback string) string {
	if refNamespace != "" {
		return refNamespace
	}
	if fallback != "" {
		return fallback
	}
	return v1alpha1.DefaultNamespace
}
