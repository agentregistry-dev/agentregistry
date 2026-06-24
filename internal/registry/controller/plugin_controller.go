package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"k8s.io/client-go/util/workqueue"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/source"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// PluginControllerDeps are the Plugin controller's dependencies. Resolver pins
// a plugin's source pointer and loads its bundle; it is required.
type PluginControllerDeps struct {
	Resolver source.Resolver
}

// pluginStore is the subset of *v1alpha1store.Store the controller uses,
// expressed as an interface so reconcile/patchStatus can be tested with a fake
// (no database). *v1alpha1store.Store satisfies it.
type pluginStore interface {
	Get(ctx context.Context, namespace, name, tag string) (*v1alpha1.RawObject, error)
	List(ctx context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error)
	ApplyPatch(ctx context.Context, namespace, name, tag string, patch v1alpha1store.PatchOpts) error
}

type pluginQueueKey struct {
	Namespace string
	Name      string
	Tag       string
}

// PluginController reconciles Plugin resources out of band of the API write: it
// resolves each plugin's pinned source pointer (a git commit or — later — an
// OCI digest) to a concrete commit/digest, scans the source for its manifest
// and inventory, and records all of it in PluginStatus. It stores NOTHING: the
// bundle stays at its origin and is materialized from source at deploy time.
//
// It is level-triggered — every control-plane wakeup (and the resync tick)
// re-lists plugins and enqueues those whose status is behind their generation.
// Status writes never re-emit control-plane events (the trigger skips
// spec-equal updates), so the controller does not wake itself. Each controller
// opens its OWN control-plane LISTEN subscription (the Deployment controller has
// a separate one); there is no shared listen loop.
type PluginController struct {
	Store    pluginStore
	Resolver source.Resolver
	Wakeups  <-chan struct{}

	queueMu sync.Mutex
	queue   workqueue.TypedRateLimitingInterface[pluginQueueKey]
}

// StartPluginController wires the Plugin controller and starts it in the
// background, opening its own control-plane LISTEN subscription (same mechanism
// as the Deployment controller).
func StartPluginController(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	deps PluginControllerDeps,
) (*PluginController, error) {
	if pool == nil {
		return nil, nil
	}
	store := stores[v1alpha1.KindPlugin]
	if store == nil {
		return nil, errors.New("plugin controller: Plugin store is required")
	}
	if deps.Resolver == nil {
		return nil, errors.New("plugin controller: Resolver is required")
	}
	c := &PluginController{Store: store, Resolver: deps.Resolver, Wakeups: controlPlaneWakeups(ctx, pool)}
	go func() {
		if err := c.Run(ctx, defaultControllerResyncInterval); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("plugin controller stopped", "error", err)
		}
	}()
	return c, nil
}

func (c *PluginController) workQueue() workqueue.TypedRateLimitingInterface[pluginQueueKey] {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	if c.queue == nil {
		c.queue = workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[pluginQueueKey](),
			workqueue.TypedRateLimitingQueueConfig[pluginQueueKey]{Name: "plugin-controller"},
		)
	}
	return c.queue
}

// Run drives the controller loop until ctx is cancelled.
func (c *PluginController) Run(ctx context.Context, resync time.Duration) error {
	if c == nil || c.Store == nil {
		return errors.New("plugin controller: Plugin store is required")
	}
	if c.Resolver == nil {
		return errors.New("plugin controller: Resolver is required")
	}
	queue := c.workQueue()
	defer queue.ShutDown()

	workerErrs := make(chan error, 1)
	go func() { workerErrs <- c.runWorker(ctx) }()

	c.enqueueAllLogged(ctx)

	var ticks <-chan time.Time
	if resync > 0 {
		ticker := time.NewTicker(resync)
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
			c.enqueueAllLogged(ctx)
		case <-ticks:
			c.enqueueAllLogged(ctx)
		}
	}
}

// enqueueAllLogged runs an enqueue pass, logging (not propagating) a failure so
// a transient list/decode error cannot kill the controller — the next
// wakeup/resync tick retries. Mirrors DeploymentDiscoveryController.Run.
func (c *PluginController) enqueueAllLogged(ctx context.Context) {
	if err := c.enqueueAll(ctx); err != nil {
		logger.Error("plugin controller: enqueue pass failed (will retry on next tick)", "error", err)
	}
}

func (c *PluginController) runWorker(ctx context.Context) error {
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

func (c *PluginController) processQueueItem(ctx context.Context, queue workqueue.TypedRateLimitingInterface[pluginQueueKey], key pluginQueueKey) {
	defer queue.Done(key)
	outcome, message, err := c.reconcileKey(ctx, key)
	if err != nil {
		// Retryable (origin/registry outage): back off and retry.
		logger.Error("plugin reconcile failed", "namespace", key.Namespace, "name", key.Name, "tag", key.Tag, "error", err)
		queue.AddRateLimited(key)
		return
	}
	queue.Forget(key)
	if outcome != "" {
		logger.Debug("plugin reconciled", "namespace", key.Namespace, "name", key.Name, "tag", key.Tag, "outcome", outcome, "message", message)
	}
}

// enqueueAll lists plugins and enqueues those not yet reconciled for their
// current generation. The workqueue coalesces duplicate keys.
func (c *PluginController) enqueueAll(ctx context.Context) error {
	queue := c.workQueue()
	opts := v1alpha1store.ListOpts{Limit: defaultControllerListPageSize}
	for {
		rows, cursor, err := c.Store.List(ctx, opts)
		if err != nil {
			return fmt.Errorf("plugin controller: list plugins: %w", err)
		}
		for _, raw := range rows {
			p, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Plugin { return &v1alpha1.Plugin{} }, raw, v1alpha1.KindPlugin)
			if err != nil {
				// One unparseable row must not halt reconciliation of all the
				// others; skip it (it cannot be acted on) and continue.
				logger.Error("plugin controller: skipping undecodable plugin row", "error", err)
				continue
			}
			if pluginReconciled(p) {
				continue
			}
			queue.Add(pluginQueueKey{Namespace: p.Metadata.NamespaceOrDefault(), Name: p.Metadata.Name, Tag: p.Metadata.Tag})
		}
		if cursor == "" {
			return nil
		}
		opts.Cursor = cursor
	}
}

const pluginReadyCondition = "Ready"

// pluginReconciled reports whether the controller has already acted on the
// plugin's current generation. It gates on ObservedGeneration ALONE (not
// Ready), because both success and terminal failure advance ObservedGeneration
// — a terminally-failed plugin must NOT be re-resolved on every resync tick.
// Retryable failures intentionally leave ObservedGeneration behind so they are
// re-enqueued (and the workqueue rate-limiter backs them off).
func pluginReconciled(p *v1alpha1.Plugin) bool {
	return p.Metadata.Generation > 0 && p.Status.ObservedGeneration >= p.Metadata.Generation
}

func (c *PluginController) reconcileKey(ctx context.Context, key pluginQueueKey) (outcome, message string, err error) {
	if key.Tag == "" {
		return "", "", fmt.Errorf("plugin controller: empty tag for %s/%s", key.Namespace, key.Name)
	}
	raw, err := c.Store.Get(ctx, key.Namespace, key.Name, key.Tag)
	if errors.Is(err, pkgdb.ErrNotFound) {
		return "missing", "plugin row no longer exists", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("plugin controller: load %s/%s:%s: %w", key.Namespace, key.Name, key.Tag, err)
	}
	p, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Plugin { return &v1alpha1.Plugin{} }, raw, v1alpha1.KindPlugin)
	if err != nil {
		return "", "", fmt.Errorf("plugin controller: decode %s/%s:%s: %w", key.Namespace, key.Name, key.Tag, err)
	}
	if pluginReconciled(p) {
		return "skipped", "up to date", nil
	}
	return c.reconcile(ctx, p)
}

// reconcile resolves+pins the plugin's origin, scans manifest/inventory, and
// patches status. It returns a non-nil error only for RETRYABLE failures
// (origin outage) so the queue applies rate-limited backoff; terminal failures
// (unsupported origin, malformed bundle) are surfaced as a status condition
// with a nil error (Forget).
func (c *PluginController) reconcile(ctx context.Context, p *v1alpha1.Plugin) (string, string, error) {
	gen := p.Metadata.Generation
	ns, name, tag := p.Metadata.NamespaceOrDefault(), p.Metadata.Name, p.Metadata.Tag

	// First observe: announce Progressing (no observedGeneration bump, so a
	// crash mid-reconcile re-runs). On retries the condition already carries the
	// last failure reason; don't flap it back to Progressing.
	if p.Status.GetCondition(pluginReadyCondition) == nil {
		if err := c.patchStatus(ctx, ns, name, tag, 0, func(st *v1alpha1.PluginStatus) {
			setReady(st, v1alpha1.ConditionFalse, "Progressing", "resolving plugin source")
		}); err != nil {
			return "", "", err
		}
	}

	resolved, b, err := c.Resolver.Resolve(ctx, p)
	if err != nil {
		reason, terminal := classifyResolveErr(err)
		bump := int64(0)
		if terminal {
			bump = gen
		}
		patchErr := c.patchStatus(ctx, ns, name, tag, bump, func(st *v1alpha1.PluginStatus) {
			setReady(st, v1alpha1.ConditionFalse, reason, err.Error())
		})
		if terminal {
			return "failed", reason, patchErr
		}
		return "", "", err // retryable
	}

	manifest, err := bundle.ParseManifest(b)
	if err != nil {
		return "failed", "SourceInvalid", c.patchStatus(ctx, ns, name, tag, gen, func(st *v1alpha1.PluginStatus) {
			setReady(st, v1alpha1.ConditionFalse, "SourceInvalid", err.Error())
		})
	}
	inventory := bundle.BuildInventory(b)

	return "resolved", "", c.patchStatus(ctx, ns, name, tag, gen, func(st *v1alpha1.PluginStatus) {
		st.ResolvedSource = resolved
		st.Manifest = manifest
		st.Inventory = inventory
		setReady(st, v1alpha1.ConditionTrue, "Resolved", "")
	})
}

// classifyResolveErr maps a resolver error to a status reason and whether it is
// terminal (Forget) or retryable (rate-limited requeue).
func classifyResolveErr(err error) (reason string, terminal bool) {
	switch {
	case errors.Is(err, source.ErrUnsupportedSource):
		return "SourceUnsupported", true
	case errors.Is(err, source.ErrSourceNotFound):
		return "RefNotFound", true
	case errors.Is(err, bundle.ErrInvalidBundle):
		return "SourceInvalid", true
	default:
		return "SourceUnresolvable", false
	}
}

func setReady(st *v1alpha1.PluginStatus, status v1alpha1.ConditionStatus, reason, message string) {
	st.SetCondition(v1alpha1.Condition{Type: pluginReadyCondition, Status: status, Reason: reason, Message: message})
}

// patchStatus applies a status mutation out of band via the raw-JSON patch
// callback. bumpGen>0 advances ObservedGeneration (success / terminal paths);
// 0 leaves it unchanged (Progressing / retryable paths) so the plugin
// re-reconciles.
func (c *PluginController) patchStatus(ctx context.Context, ns, name, tag string, bumpGen int64, mutate func(*v1alpha1.PluginStatus)) error {
	return c.Store.ApplyPatch(ctx, ns, name, tag, v1alpha1store.PatchOpts{
		Status: func(current json.RawMessage) (json.RawMessage, error) {
			tmp := &v1alpha1.Plugin{}
			if err := tmp.UnmarshalStatus(current); err != nil {
				return nil, err
			}
			mutate(&tmp.Status)
			if bumpGen > 0 && tmp.Status.ObservedGeneration < bumpGen {
				tmp.Status.ObservedGeneration = bumpGen
			}
			return tmp.MarshalStatus()
		},
	})
}
