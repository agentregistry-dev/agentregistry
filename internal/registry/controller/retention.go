package controller

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RetentionPolicy is the bounded-history contract for the controller
// foundation tables. Durations <= 0 disable pruning for that table.
type RetentionPolicy struct {
	ControlPlaneEvents time.Duration
	EventKeepAfterRev  int64
	ReconcileWork      time.Duration
	ReconcileAttempts  time.Duration
	BatchLimit         int
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

// RunRetentionPrune applies a RetentionPolicy to the controller foundation
// tables. It is intentionally side-effect-only on bookkeeping tables:
// canonical resource tables remain the source of truth, so projectors can full
// resync if their checkpoint falls behind the retained event range.
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
