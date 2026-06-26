package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// fakeSkillStore captures status patches by replaying the raw-JSON callback, so
// reconcile/patchStatus can be tested with no database.
type fakeSkillStore struct {
	status   map[string]json.RawMessage
	reasons  []string // Ready-condition reasons, in apply order
	listRows []*v1alpha1.RawObject
}

func newFakeSkillStore() *fakeSkillStore {
	return &fakeSkillStore{status: map[string]json.RawMessage{}}
}

func (f *fakeSkillStore) key(ns, name, tag string) string { return ns + "/" + name + ":" + tag }

func (f *fakeSkillStore) Get(context.Context, string, string, string) (*v1alpha1.RawObject, error) {
	return nil, pkgdb.ErrNotFound
}

func (f *fakeSkillStore) List(context.Context, v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error) {
	return f.listRows, "", nil // single page
}

func (f *fakeSkillStore) ApplyPatch(_ context.Context, ns, name, tag string, patch v1alpha1store.PatchOpts) error {
	k := f.key(ns, name, tag)
	out, err := patch.Status(f.status[k])
	if err != nil {
		return err
	}
	f.status[k] = out
	tmp := &v1alpha1.Skill{}
	if err := tmp.UnmarshalStatus(out); err != nil {
		return err
	}
	if cond := tmp.Status.GetCondition(skillReadyCondition); cond != nil {
		f.reasons = append(f.reasons, cond.Reason)
	}
	return nil
}

func (f *fakeSkillStore) skill(t *testing.T, ns, name, tag string) *v1alpha1.Skill {
	t.Helper()
	s := &v1alpha1.Skill{}
	if err := s.UnmarshalStatus(f.status[f.key(ns, name, tag)]); err != nil {
		t.Fatal(err)
	}
	return s
}

func skillReadyReason(s *v1alpha1.Skill) string {
	if c := s.Status.GetCondition(skillReadyCondition); c != nil {
		return c.Reason
	}
	return ""
}

func TestClassifySkillResolveErr(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantReason   string
		wantTerminal bool
	}{
		{"unsupported host", fmt.Errorf("wrap: %w", gitutil.ErrUnsupportedHost), "SourceUnsupported", true},
		{"ref not found", fmt.Errorf("wrap: %w", gitutil.ErrRefNotFound), "RefNotFound", true},
		{"transient", errors.New("dial tcp: timeout"), "SourceUnresolvable", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, terminal := classifySkillResolveErr(tt.err)
			if reason != tt.wantReason || terminal != tt.wantTerminal {
				t.Fatalf("classifySkillResolveErr = (%q, %v), want (%q, %v)", reason, terminal, tt.wantReason, tt.wantTerminal)
			}
		})
	}
}

func TestSkillReconciled(t *testing.T) {
	skill := func(observed, gen int64, ready v1alpha1.ConditionStatus) *v1alpha1.Skill {
		s := &v1alpha1.Skill{}
		s.Metadata.Generation = gen
		s.Status.ObservedGeneration = observed
		s.Status.SetCondition(v1alpha1.Condition{Type: skillReadyCondition, Status: ready, Reason: "x"})
		return s
	}

	// Gates on ObservedGeneration only; Ready true/false is irrelevant.
	if !skillReconciled(skill(3, 3, v1alpha1.ConditionTrue)) {
		t.Fatal("observed==gen (success) should be reconciled")
	}
	if !skillReconciled(skill(3, 3, v1alpha1.ConditionFalse)) {
		t.Fatal("observed==gen (terminal failure) should be reconciled — must NOT re-resolve every tick")
	}
	if skillReconciled(skill(2, 3, v1alpha1.ConditionFalse)) {
		t.Fatal("observed<gen (retryable / pending) should NOT be reconciled")
	}
	if skillReconciled(&v1alpha1.Skill{}) {
		t.Fatal("a fresh skill (generation 0 in this zero value) should NOT be reconciled")
	}
}

// TestSkillEnqueueAllSkipsUndecodableRow guards the resilience behavior: one row
// that fails to decode must be skipped (logged), not abort the whole pass.
func TestSkillEnqueueAllSkipsUndecodableRow(t *testing.T) {
	rawOf := func(name string, spec string) *v1alpha1.RawObject {
		return &v1alpha1.RawObject{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindSkill},
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name, Tag: "v1", Generation: 1},
			Spec:     json.RawMessage(spec),
		}
	}
	store := newFakeSkillStore()
	store.listRows = []*v1alpha1.RawObject{
		rawOf("bad", `not json`), // EnvelopeFromRaw fails -> skip
		rawOf("good", `{"source":{"repository":{"url":"https://github.com/o/r"}}}`), // valid, needs reconcile -> enqueue
	}
	c := &SkillController{Store: store}

	if err := c.enqueueAll(context.Background()); err != nil {
		t.Fatalf("enqueueAll must not error on an undecodable row, got %v", err)
	}
	if n := c.workQueue().Len(); n != 1 {
		t.Fatalf("expected only the good row enqueued, queue len = %d", n)
	}
}

func TestSkillReconcile(t *testing.T) {
	const ns, name, tag = "default", "s", "v1"
	newSkill := func(gen int64) *v1alpha1.Skill {
		s := &v1alpha1.Skill{Metadata: v1alpha1.ObjectMeta{Namespace: ns, Name: name, Tag: tag, Generation: gen}}
		s.Spec.Source = &v1alpha1.SkillSource{
			Repository: &v1alpha1.Repository{URL: "https://github.com/o/r", Branch: "main"},
		}
		return s
	}
	resolveTo := func(commit string) SkillResolveFunc {
		return func(context.Context, *v1alpha1.Repository) (string, error) { return commit, nil }
	}
	resolveErr := func(err error) SkillResolveFunc {
		return func(context.Context, *v1alpha1.Repository) (string, error) { return "", err }
	}

	t.Run("success transitions Progressing then Resolved and bumps observedGeneration", func(t *testing.T) {
		store := newFakeSkillStore()
		c := &SkillController{Store: store, Resolve: resolveTo("deadbeef")}
		outcome, _, err := c.reconcile(context.Background(), newSkill(2))
		if err != nil || outcome != "resolved" {
			t.Fatalf("reconcile = (%q, %v), want (resolved, nil)", outcome, err)
		}
		if !reflect.DeepEqual(store.reasons, []string{"Progressing", "Resolved"}) {
			t.Fatalf("reason sequence = %v, want [Progressing Resolved]", store.reasons)
		}
		got := store.skill(t, ns, name, tag)
		if !got.Status.IsConditionTrue(skillReadyCondition) {
			t.Error("expected Ready=True")
		}
		if got.Status.ObservedGeneration != 2 {
			t.Errorf("observedGeneration = %d, want 2", got.Status.ObservedGeneration)
		}
		if got.Status.ResolvedSource == nil || got.Status.ResolvedSource.Commit != "deadbeef" {
			t.Errorf("resolvedSource = %+v", got.Status.ResolvedSource)
		}
	})

	t.Run("terminal unsupported host forgets and bumps observedGeneration", func(t *testing.T) {
		store := newFakeSkillStore()
		c := &SkillController{Store: store, Resolve: resolveErr(fmt.Errorf("x: %w", gitutil.ErrUnsupportedHost))}
		outcome, reason, err := c.reconcile(context.Background(), newSkill(3))
		if err != nil {
			t.Fatalf("terminal failure must return nil error (Forget), got %v", err)
		}
		if outcome != "failed" || reason != "SourceUnsupported" {
			t.Fatalf("got (%q, %q), want (failed, SourceUnsupported)", outcome, reason)
		}
		got := store.skill(t, ns, name, tag)
		if got.Status.ObservedGeneration != 3 {
			t.Errorf("terminal must bump observedGeneration, got %d", got.Status.ObservedGeneration)
		}
		if got.Status.IsConditionTrue(skillReadyCondition) {
			t.Error("must not be Ready")
		}
	})

	t.Run("retryable failure requeues and leaves observedGeneration behind", func(t *testing.T) {
		store := newFakeSkillStore()
		c := &SkillController{Store: store, Resolve: resolveErr(errors.New("dial tcp: timeout"))}
		_, _, err := c.reconcile(context.Background(), newSkill(4))
		if err == nil {
			t.Fatal("retryable failure must return a non-nil error (requeue)")
		}
		got := store.skill(t, ns, name, tag)
		if got.Status.ObservedGeneration != 0 {
			t.Errorf("retryable must NOT bump observedGeneration, got %d", got.Status.ObservedGeneration)
		}
		if r := skillReadyReason(got); r != "SourceUnresolvable" {
			t.Errorf("ready reason = %q, want SourceUnresolvable", r)
		}
	})

	t.Run("missing source is terminal SourceMissing", func(t *testing.T) {
		store := newFakeSkillStore()
		c := &SkillController{Store: store, Resolve: resolveTo("never-called")}
		s := &v1alpha1.Skill{Metadata: v1alpha1.ObjectMeta{Namespace: ns, Name: name, Tag: tag, Generation: 6}}
		outcome, reason, err := c.reconcile(context.Background(), s)
		if err != nil {
			t.Fatalf("terminal must Forget, got %v", err)
		}
		if outcome != "failed" || reason != "SourceMissing" {
			t.Fatalf("got (%q, %q), want (failed, SourceMissing)", outcome, reason)
		}
		got := store.skill(t, ns, name, tag)
		if got.Status.ObservedGeneration != 6 {
			t.Errorf("observedGeneration = %d, want 6", got.Status.ObservedGeneration)
		}
		if got.Status.ResolvedSource != nil {
			t.Errorf("missing-source must not pin a commit, got %+v", got.Status.ResolvedSource)
		}
	})
}
