package controller

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const defaultRetentionPruneInterval = time.Hour

// RetentionPolicy is the bounded-history contract for the controller
// foundation tables. Durations <= 0 disable pruning for that table.
type RetentionPolicy struct {
	ControlPlaneEvents time.Duration
	EventKeepAfterRev  int64
	ReconcileWork      time.Duration
	ReconcileAttempts  time.Duration
	BatchLimit         int
}

// Enabled reports whether the policy prunes at least one controller table.
func (p RetentionPolicy) Enabled() bool {
	return p.ControlPlaneEvents > 0 || p.ReconcileWork > 0 || p.ReconcileAttempts > 0
}

// PruneStores groups the store surfaces needed by RunRetentionPrune. Keeping
// these as tiny interfaces lets the controller package stay independent from
// concrete Postgres store construction and keeps tests cheap.
type PruneStores struct {
	ControlPlaneEvents interface {
		PruneBefore(ctx context.Context, before time.Time, keepAfterRevision int64, limit int) (int64, error)
	}
	ReconcileWork interface {
		Prune(ctx context.Context, before time.Time, limit int) (int64, error)
	}
	ReconcileAttempts interface {
		Prune(ctx context.Context, before time.Time, limit int) (int64, error)
	}
}

// RetentionPruneResult reports how many rows were removed from each
// controller foundation table in one maintenance pass.
type RetentionPruneResult struct {
	ControlPlaneEvents int64
	ReconcileWork      int64
	ReconcileAttempts  int64
}

// RetentionPruner owns the periodic maintenance loop for controller
// bookkeeping tables.
type RetentionPruner struct {
	Stores PruneStores
	Policy RetentionPolicy
	Now    func() time.Time
}

func (p *RetentionPruner) Enabled() bool {
	return p != nil && p.Policy.Enabled()
}

func (p *RetentionPruner) RunOnce(ctx context.Context) (RetentionPruneResult, error) {
	if p == nil {
		return RetentionPruneResult{}, errors.New("controller retention: pruner is required")
	}
	return RunRetentionPrune(ctx, p.Stores, p.Policy, p.now())
}

func (p *RetentionPruner) Run(ctx context.Context, interval time.Duration) error {
	if p == nil {
		return errors.New("controller retention: pruner is required")
	}
	if interval <= 0 {
		interval = defaultRetentionPruneInterval
	}
	p.runOnceLogged(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			p.runOnceLogged(ctx)
		}
	}
}

func (p *RetentionPruner) now() time.Time {
	if p != nil && p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p *RetentionPruner) runOnceLogged(ctx context.Context) {
	result, err := p.RunOnce(ctx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			logger.Error("deployment controller retention prune failed", "error", err)
		}
		return
	}
	if result != (RetentionPruneResult{}) {
		logger.Info(
			"deployment controller retention pruned bookkeeping rows",
			"control_plane_events", result.ControlPlaneEvents,
			"reconcile_work", result.ReconcileWork,
			"reconcile_attempts", result.ReconcileAttempts,
		)
	}
}

// RunRetentionPrune applies a RetentionPolicy to the controller foundation
// tables. It is intentionally side-effect-only on bookkeeping tables:
// canonical resource tables remain the source of truth, so controllers can
// full-reconcile if their checkpoint falls behind the retained event range.
func RunRetentionPrune(ctx context.Context, stores PruneStores, policy RetentionPolicy, now time.Time) (RetentionPruneResult, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	limit := policy.BatchLimit
	var (
		result RetentionPruneResult
		errs   error
	)

	if stores.ControlPlaneEvents != nil && policy.ControlPlaneEvents > 0 {
		n, err := stores.ControlPlaneEvents.PruneBefore(ctx, now.Add(-policy.ControlPlaneEvents), policy.EventKeepAfterRev, limit)
		result.ControlPlaneEvents = n
		errs = errors.Join(errs, wrapRetentionErr("prune control-plane events", err))
	}
	if stores.ReconcileWork != nil && policy.ReconcileWork > 0 {
		n, err := stores.ReconcileWork.Prune(ctx, now.Add(-policy.ReconcileWork), limit)
		result.ReconcileWork = n
		errs = errors.Join(errs, wrapRetentionErr("prune reconcile work", err))
	}
	if stores.ReconcileAttempts != nil && policy.ReconcileAttempts > 0 {
		n, err := stores.ReconcileAttempts.Prune(ctx, now.Add(-policy.ReconcileAttempts), limit)
		result.ReconcileAttempts = n
		errs = errors.Join(errs, wrapRetentionErr("prune reconcile attempts", err))
	}
	return result, errs
}

func wrapRetentionErr(context string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, err)
}
