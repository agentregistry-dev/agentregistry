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

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// SkillResolveFunc resolves a skill's git source ref to a concrete commit SHA.
// It is the only I/O-bearing dependency of the Skill controller, so tests inject
// a fake instead of touching the network.
type SkillResolveFunc func(ctx context.Context, repo *v1alpha1.Repository) (commit string, err error)

// SkillControllerDeps are the Skill controller's dependencies. Resolve pins a
// skill's git source ref to a commit; it defaults to a git ls-remote resolver
// when nil.
type SkillControllerDeps struct {
	Resolve SkillResolveFunc
}

// defaultSkillResolve pins a skill's git source by resolving its ref (an
// explicit commit, else the branch, else the repo/HEAD default) to a concrete
// commit SHA via ls-remote. It is intentionally lighter than the Plugin
// resolver: a skill has no manifest/inventory to scan, so the controller only
// needs the pin. Materialization (the actual checkout) happens at deploy time.
func defaultSkillResolve(ctx context.Context, repo *v1alpha1.Repository) (string, error) {
	ref := repo.Commit
	if ref == "" {
		ref = repo.Branch
	}
	ctx, cancel := context.WithTimeout(ctx, skillResolveTimeout)
	defer cancel()
	return gitutil.ResolveRefContext(ctx, repo.URL, ref)
}

const skillResolveTimeout = 2 * time.Minute

// skillStore is the subset of *v1alpha1store.Store the controller uses,
// expressed as an interface so reconcile/patchStatus can be tested with a fake
// (no database). *v1alpha1store.Store satisfies it.
type skillStore interface {
	Get(ctx context.Context, namespace, name, tag string) (*v1alpha1.RawObject, error)
	List(ctx context.Context, opts v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error)
	ApplyPatch(ctx context.Context, namespace, name, tag string, patch v1alpha1store.PatchOpts) error
}

type skillQueueKey struct {
	Namespace string
	Name      string
	Tag       string
}

// SkillController reconciles Skill resources out of band of the API write: it
// resolves each skill's pinned git source ref to a concrete commit and records
// it in SkillStatus.ResolvedSource. It stores NOTHING: the skill content stays
// at its origin and is materialized from source at deploy time. It mirrors the
// Plugin controller's resolve-and-pin model, minus the manifest/inventory scan
// (a skill has no bundle to enumerate).
//
// It is level-triggered — every control-plane wakeup (and the resync tick)
// re-lists skills and enqueues those whose status is behind their generation.
// Status writes never re-emit control-plane events (the trigger skips spec-equal
// updates), so the controller does not wake itself. Each controller opens its
// OWN control-plane LISTEN subscription.
type SkillController struct {
	Store   skillStore
	Resolve SkillResolveFunc
	Wakeups <-chan struct{}

	queueMu sync.Mutex
	queue   workqueue.TypedRateLimitingInterface[skillQueueKey]
}

// StartSkillController wires the Skill controller and starts it in the
// background, opening its own control-plane LISTEN subscription (same mechanism
// as the Plugin and Deployment controllers).
func StartSkillController(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	deps SkillControllerDeps,
) (*SkillController, error) {
	if pool == nil {
		return nil, nil
	}
	store := stores[v1alpha1.KindSkill]
	if store == nil {
		return nil, errors.New("skill controller: Skill store is required")
	}
	resolve := deps.Resolve
	if resolve == nil {
		resolve = defaultSkillResolve
	}
	c := &SkillController{Store: store, Resolve: resolve, Wakeups: controlPlaneWakeups(ctx, pool)}
	go func() {
		if err := c.Run(ctx, defaultControllerResyncInterval); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("skill controller stopped", "error", err)
		}
	}()
	return c, nil
}

func (c *SkillController) workQueue() workqueue.TypedRateLimitingInterface[skillQueueKey] {
	c.queueMu.Lock()
	defer c.queueMu.Unlock()
	if c.queue == nil {
		c.queue = workqueue.NewTypedRateLimitingQueueWithConfig(
			workqueue.DefaultTypedControllerRateLimiter[skillQueueKey](),
			workqueue.TypedRateLimitingQueueConfig[skillQueueKey]{Name: "skill-controller"},
		)
	}
	return c.queue
}

// Run drives the controller loop until ctx is cancelled.
func (c *SkillController) Run(ctx context.Context, resync time.Duration) error {
	if c == nil || c.Store == nil {
		return errors.New("skill controller: Skill store is required")
	}
	if c.Resolve == nil {
		c.Resolve = defaultSkillResolve
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
// wakeup/resync tick retries.
func (c *SkillController) enqueueAllLogged(ctx context.Context) {
	if err := c.enqueueAll(ctx); err != nil {
		logger.Error("skill controller: enqueue pass failed (will retry on next tick)", "error", err)
	}
}

func (c *SkillController) runWorker(ctx context.Context) error {
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

func (c *SkillController) processQueueItem(ctx context.Context, queue workqueue.TypedRateLimitingInterface[skillQueueKey], key skillQueueKey) {
	defer queue.Done(key)
	outcome, message, err := c.reconcileKey(ctx, key)
	if err != nil {
		// Retryable (origin outage): back off and retry.
		logger.Error("skill reconcile failed", "namespace", key.Namespace, "name", key.Name, "tag", key.Tag, "error", err)
		queue.AddRateLimited(key)
		return
	}
	queue.Forget(key)
	if outcome != "" {
		logger.Debug("skill reconciled", "namespace", key.Namespace, "name", key.Name, "tag", key.Tag, "outcome", outcome, "message", message)
	}
}

// enqueueAll lists skills and enqueues those not yet reconciled for their
// current generation. The workqueue coalesces duplicate keys.
func (c *SkillController) enqueueAll(ctx context.Context) error {
	queue := c.workQueue()
	opts := v1alpha1store.ListOpts{Limit: defaultControllerListPageSize}
	for {
		rows, cursor, err := c.Store.List(ctx, opts)
		if err != nil {
			return fmt.Errorf("skill controller: list skills: %w", err)
		}
		for _, raw := range rows {
			sk, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Skill { return &v1alpha1.Skill{} }, raw, v1alpha1.KindSkill)
			if err != nil {
				// One unparseable row must not halt reconciliation of all the
				// others; skip it (it cannot be acted on) and continue.
				logger.Error("skill controller: skipping undecodable skill row", "error", err)
				continue
			}
			if skillReconciled(sk) {
				continue
			}
			queue.Add(skillQueueKey{Namespace: sk.Metadata.NamespaceOrDefault(), Name: sk.Metadata.Name, Tag: sk.Metadata.Tag})
		}
		if cursor == "" {
			return nil
		}
		opts.Cursor = cursor
	}
}

const skillReadyCondition = "Ready"

// skillReconciled reports whether the controller has already acted on the
// skill's current generation. It gates on ObservedGeneration ALONE (not Ready),
// because both success and terminal failure advance ObservedGeneration — a
// terminally-failed skill must NOT be re-resolved on every resync tick.
// Retryable failures intentionally leave ObservedGeneration behind so they are
// re-enqueued (and the workqueue rate-limiter backs them off).
func skillReconciled(sk *v1alpha1.Skill) bool {
	return sk.Metadata.Generation > 0 && sk.Status.ObservedGeneration >= sk.Metadata.Generation
}

func (c *SkillController) reconcileKey(ctx context.Context, key skillQueueKey) (outcome, message string, err error) {
	if key.Tag == "" {
		return "", "", fmt.Errorf("skill controller: empty tag for %s/%s", key.Namespace, key.Name)
	}
	raw, err := c.Store.Get(ctx, key.Namespace, key.Name, key.Tag)
	if errors.Is(err, pkgdb.ErrNotFound) {
		return "missing", "skill row no longer exists", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("skill controller: load %s/%s:%s: %w", key.Namespace, key.Name, key.Tag, err)
	}
	sk, err := v1alpha1.EnvelopeFromRaw(func() *v1alpha1.Skill { return &v1alpha1.Skill{} }, raw, v1alpha1.KindSkill)
	if err != nil {
		return "", "", fmt.Errorf("skill controller: decode %s/%s:%s: %w", key.Namespace, key.Name, key.Tag, err)
	}
	if skillReconciled(sk) {
		return "skipped", "up to date", nil
	}
	return c.reconcile(ctx, sk)
}

// reconcile resolves+pins the skill's git source and patches status. It returns
// a non-nil error only for RETRYABLE failures (origin outage) so the queue
// applies rate-limited backoff; terminal failures (missing/unsupported source,
// ref not found) are surfaced as a status condition with a nil error (Forget).
func (c *SkillController) reconcile(ctx context.Context, sk *v1alpha1.Skill) (string, string, error) {
	gen := sk.Metadata.Generation
	ns, name, tag := sk.Metadata.NamespaceOrDefault(), sk.Metadata.Name, sk.Metadata.Tag

	// A skill with no git source has nothing to pin — terminal, no retry.
	if sk.Spec.Source == nil || sk.Spec.Source.Repository == nil || sk.Spec.Source.Repository.URL == "" {
		return "failed", "SourceMissing", c.patchStatus(ctx, ns, name, tag, gen, func(st *v1alpha1.SkillStatus) {
			setSkillReady(st, v1alpha1.ConditionFalse, "SourceMissing", "skill has no source.repository.url to resolve")
		})
	}

	// First observe: announce Progressing (no observedGeneration bump, so a
	// crash mid-reconcile re-runs). On retries the condition already carries the
	// last failure reason; don't flap it back to Progressing.
	if sk.Status.GetCondition(skillReadyCondition) == nil {
		if err := c.patchStatus(ctx, ns, name, tag, 0, func(st *v1alpha1.SkillStatus) {
			setSkillReady(st, v1alpha1.ConditionFalse, "Progressing", "resolving skill source")
		}); err != nil {
			return "", "", err
		}
	}

	commit, err := c.Resolve(ctx, sk.Spec.Source.Repository)
	if err != nil {
		reason, terminal := classifySkillResolveErr(err)
		bump := int64(0)
		if terminal {
			bump = gen
		}
		patchErr := c.patchStatus(ctx, ns, name, tag, bump, func(st *v1alpha1.SkillStatus) {
			setSkillReady(st, v1alpha1.ConditionFalse, reason, err.Error())
		})
		if terminal {
			return "failed", reason, patchErr
		}
		return "", "", err // retryable
	}

	return "resolved", "", c.patchStatus(ctx, ns, name, tag, gen, func(st *v1alpha1.SkillStatus) {
		st.ResolvedSource = &v1alpha1.SkillResolvedSource{Commit: commit}
		setSkillReady(st, v1alpha1.ConditionTrue, "Resolved", "")
	})
}

// classifySkillResolveErr maps a resolve error to a status reason and whether it
// is terminal (Forget) or retryable (rate-limited requeue).
func classifySkillResolveErr(err error) (reason string, terminal bool) {
	switch {
	case errors.Is(err, gitutil.ErrUnsupportedHost):
		return "SourceUnsupported", true
	case errors.Is(err, gitutil.ErrRefNotFound):
		return "RefNotFound", true
	default:
		return "SourceUnresolvable", false
	}
}

func setSkillReady(st *v1alpha1.SkillStatus, status v1alpha1.ConditionStatus, reason, message string) {
	st.SetCondition(v1alpha1.Condition{Type: skillReadyCondition, Status: status, Reason: reason, Message: message})
}

// patchStatus applies a status mutation out of band via the raw-JSON patch
// callback. bumpGen>0 advances ObservedGeneration (success / terminal paths);
// 0 leaves it unchanged (Progressing / retryable paths) so the skill
// re-reconciles.
func (c *SkillController) patchStatus(ctx context.Context, ns, name, tag string, bumpGen int64, mutate func(*v1alpha1.SkillStatus)) error {
	return c.Store.ApplyPatch(ctx, ns, name, tag, v1alpha1store.PatchOpts{
		Status: func(current json.RawMessage) (json.RawMessage, error) {
			tmp := &v1alpha1.Skill{}
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
