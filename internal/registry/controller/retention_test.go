package controller

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestRunRetentionPruneAppliesConfiguredEventCutoff(t *testing.T) {
	now := time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC)
	events := &fakeEventPruner{deleted: 2}

	result, err := RunRetentionPrune(context.Background(), PruneStores{
		ControlPlaneEvents: events,
	}, RetentionPolicy{
		ControlPlaneEvents: 2 * time.Hour,
		EventKeepAfterRev:  42,
		BatchLimit:         17,
	}, now)
	if err != nil {
		t.Fatalf("RunRetentionPrune returned error: %v", err)
	}

	if result.ControlPlaneEvents != 2 {
		t.Fatalf("result = %+v, want event deleted count 2", result)
	}
	if events.before != now.Add(-2*time.Hour) || events.keepAfterRevision != 42 || events.limit != 17 {
		t.Fatalf("event prune args = before %s keep %d limit %d", events.before, events.keepAfterRevision, events.limit)
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
	}, RetentionPolicy{
		ControlPlaneEvents: time.Hour,
	}, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "prune control-plane events") {
		t.Fatalf("error = %q, want contextual event prune error", msg)
	}
}

func TestRetentionPolicyEnabled(t *testing.T) {
	tests := []struct {
		name   string
		policy RetentionPolicy
		want   bool
	}{
		{name: "empty", policy: RetentionPolicy{}, want: false},
		{name: "events", policy: RetentionPolicy{ControlPlaneEvents: time.Hour}, want: true},
		{name: "revision bound alone does not enable age pruning", policy: RetentionPolicy{EventKeepAfterRev: 42}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.policy.Enabled(); got != tt.want {
				t.Fatalf("Enabled() = %t, want %t", got, tt.want)
			}
		})
	}
}

func TestRetentionPrunerRunOnceUsesPolicy(t *testing.T) {
	now := time.Date(2026, 5, 26, 9, 30, 0, 0, time.UTC)
	events := &fakeEventPruner{deleted: 7}
	pruner := &RetentionPruner{
		Stores: PruneStores{ControlPlaneEvents: events},
		Policy: RetentionPolicy{
			ControlPlaneEvents: time.Hour,
			EventKeepAfterRev:  11,
			BatchLimit:         13,
		},
		Now: func() time.Time { return now },
	}

	result, err := pruner.RunOnce(context.Background())
	if err != nil {
		t.Fatalf("RunOnce returned error: %v", err)
	}
	if result.ControlPlaneEvents != 7 {
		t.Fatalf("ControlPlaneEvents = %d, want 7", result.ControlPlaneEvents)
	}
	if events.before != now.Add(-time.Hour) || events.keepAfterRevision != 11 || events.limit != 13 {
		t.Fatalf("event prune args = before %s keep %d limit %d", events.before, events.keepAfterRevision, events.limit)
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
