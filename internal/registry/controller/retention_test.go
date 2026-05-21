package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunRetentionPruneAppliesConfiguredCutoffs(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	events := &fakeEventPruner{deleted: 2}
	work := &fakePruner{deleted: 3}
	attempts := &fakePruner{deleted: 5}

	result, err := RunRetentionPrune(context.Background(), PruneStores{
		ControlPlaneEvents: events,
		ReconcileWork:      work,
		ReconcileAttempts:  attempts,
	}, RetentionPolicy{
		ControlPlaneEvents: 2 * time.Hour,
		EventKeepAfterRev:  42,
		ReconcileWork:      3 * time.Hour,
		ReconcileAttempts:  4 * time.Hour,
		BatchLimit:         17,
	}, now)
	if err != nil {
		t.Fatalf("RunRetentionPrune returned error: %v", err)
	}

	if result.ControlPlaneEvents != 2 || result.ReconcileWork != 3 || result.ReconcileAttempts != 5 {
		t.Fatalf("result = %+v, want deleted counts 2/3/5", result)
	}
	if events.before != now.Add(-2*time.Hour) || events.keepAfterRevision != 42 || events.limit != 17 {
		t.Fatalf("event prune args = before %s keep %d limit %d", events.before, events.keepAfterRevision, events.limit)
	}
	if work.before != now.Add(-3*time.Hour) || work.limit != 17 {
		t.Fatalf("work prune args = before %s limit %d", work.before, work.limit)
	}
	if attempts.before != now.Add(-4*time.Hour) || attempts.limit != 17 {
		t.Fatalf("attempt prune args = before %s limit %d", attempts.before, attempts.limit)
	}
}

func TestRunRetentionPruneSkipsDisabledPolicies(t *testing.T) {
	events := &fakeEventPruner{}

	result, err := RunRetentionPrune(context.Background(), PruneStores{
		ControlPlaneEvents: events,
	}, RetentionPolicy{}, time.Now())
	if err != nil {
		t.Fatalf("RunRetentionPrune returned error: %v", err)
	}
	if result != (RetentionPruneResult{}) {
		t.Fatalf("result = %+v, want zero", result)
	}
	if events.called {
		t.Fatal("event pruner was called for disabled retention")
	}
}

func TestRunRetentionPruneReturnsContextualErrors(t *testing.T) {
	_, err := RunRetentionPrune(context.Background(), PruneStores{
		ControlPlaneEvents: &fakeEventPruner{err: errors.New("events failed")},
		ReconcileWork:      &fakePruner{err: errors.New("work failed")},
	}, RetentionPolicy{
		ControlPlaneEvents: time.Hour,
		ReconcileWork:      time.Hour,
	}, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "prune control-plane events") || !strings.Contains(msg, "prune reconcile work") {
		t.Fatalf("error = %q, want contextual joined errors", msg)
	}
}

type fakeEventPruner struct {
	called            bool
	before            time.Time
	keepAfterRevision int64
	limit             int
	deleted           int64
	err               error
}

func (f *fakeEventPruner) PruneBefore(_ context.Context, before time.Time, keepAfterRevision int64, limit int) (int64, error) {
	f.called = true
	f.before = before
	f.keepAfterRevision = keepAfterRevision
	f.limit = limit
	return f.deleted, f.err
}

type fakePruner struct {
	before  time.Time
	limit   int
	deleted int64
	err     error
}

func (f *fakePruner) Prune(_ context.Context, before time.Time, limit int) (int64, error) {
	f.before = before
	f.limit = limit
	return f.deleted, f.err
}
