package controller

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// fakePluginStore captures status patches by replaying the raw-JSON callback,
// so reconcile/patchStatus can be tested with no database.
type fakePluginStore struct {
	status   map[string]json.RawMessage
	reasons  []string // Ready-condition reasons, in apply order
	listRows []*v1alpha1.RawObject
}

func newFakePluginStore() *fakePluginStore {
	return &fakePluginStore{status: map[string]json.RawMessage{}}
}

func (f *fakePluginStore) key(ns, name, tag string) string { return ns + "/" + name + ":" + tag }

func (f *fakePluginStore) Get(context.Context, string, string, string) (*v1alpha1.RawObject, error) {
	return nil, pkgdb.ErrNotFound
}

func (f *fakePluginStore) List(context.Context, v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error) {
	return f.listRows, "", nil // single page
}

func (f *fakePluginStore) ApplyPatch(_ context.Context, ns, name, tag string, patch v1alpha1store.PatchOpts) error {
	k := f.key(ns, name, tag)
	out, err := patch.Status(f.status[k])
	if err != nil {
		return err
	}
	f.status[k] = out
	tmp := &v1alpha1.Plugin{}
	if err := tmp.UnmarshalStatus(out); err != nil {
		return err
	}
	if cond := tmp.Status.GetCondition(pluginReadyCondition); cond != nil {
		f.reasons = append(f.reasons, cond.Reason)
	}
	return nil
}

func (f *fakePluginStore) plugin(t *testing.T, ns, name, tag string) *v1alpha1.Plugin {
	t.Helper()
	p := &v1alpha1.Plugin{}
	if err := p.UnmarshalStatus(f.status[f.key(ns, name, tag)]); err != nil {
		t.Fatal(err)
	}
	return p
}

func readyReason(p *v1alpha1.Plugin) string {
	if c := p.Status.GetCondition(pluginReadyCondition); c != nil {
		return c.Reason
	}
	return ""
}

// TestEnqueueAllSkipsUndecodableRow guards the resilience fix: one row that
// fails to decode must be skipped (logged), not abort the whole enqueue pass.
func TestEnqueueAllSkipsUndecodableRow(t *testing.T) {
	rawOf := func(name string, spec string) *v1alpha1.RawObject {
		return &v1alpha1.RawObject{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindPlugin},
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name, Tag: "v1", Generation: 1},
			Spec:     json.RawMessage(spec),
		}
	}
	store := newFakePluginStore()
	store.listRows = []*v1alpha1.RawObject{
		rawOf("bad", `not json`),                   // EnvelopeFromRaw fails -> skip
		rawOf("good", `{"source":{"type":"git"}}`), // valid, needs reconcile -> enqueue
	}
	c := &PluginController{Store: store}

	if err := c.enqueueAll(context.Background()); err != nil {
		t.Fatalf("enqueueAll must not error on an undecodable row, got %v", err)
	}
	if n := c.workQueue().Len(); n != 1 {
		t.Fatalf("expected only the good row enqueued, queue len = %d", n)
	}
}

// TestPluginReconcile drives reconcile against a real (anonymous) git source.
// Every case is chosen to fail before any network call: an unsupported source
// type never reaches git, a non-GitHub host is rejected at URL parse, and an
// option-like ref is rejected as argument injection.
func TestPluginReconcile(t *testing.T) {
	const ns, name, tag = "default", "p", "v1"
	newPlugin := func(gen int64, repo *v1alpha1.Repository) *v1alpha1.Plugin {
		p := &v1alpha1.Plugin{Metadata: v1alpha1.ObjectMeta{Namespace: ns, Name: name, Tag: tag, Generation: gen}}
		p.Spec.Source = &v1alpha1.PluginSource{
			Type: v1alpha1.PluginSourceTypeGit,
			Git:  &v1alpha1.PluginSourceGit{Repository: repo},
		}
		return p
	}
	git := gitutil.NewSource(nil)

	t.Run("terminal unsupported source announces Progressing, forgets, and bumps observedGeneration", func(t *testing.T) {
		store := newFakePluginStore()
		c := &PluginController{Store: store, Git: git}
		p := newPlugin(3, &v1alpha1.Repository{URL: "https://git.example.com/o/r", Branch: "main"})
		outcome, reason, err := c.reconcile(context.Background(), p)
		if err != nil {
			t.Fatalf("terminal failure must return nil error (Forget), got %v", err)
		}
		if outcome != "failed" || reason != "SourceUnsupported" {
			t.Fatalf("got (%q, %q), want (failed, SourceUnsupported)", outcome, reason)
		}
		// Progressing is patched before the origin is touched.
		if !reflect.DeepEqual(store.reasons, []string{"Progressing", "SourceUnsupported"}) {
			t.Fatalf("reason sequence = %v, want [Progressing SourceUnsupported]", store.reasons)
		}
		got := store.plugin(t, ns, name, tag)
		if got.Status.ObservedGeneration != 3 {
			t.Errorf("terminal must bump observedGeneration, got %d", got.Status.ObservedGeneration)
		}
		if got.Status.IsConditionTrue(pluginReadyCondition) {
			t.Error("must not be Ready")
		}
	})

	t.Run("retryable failure requeues and leaves observedGeneration behind", func(t *testing.T) {
		store := newFakePluginStore()
		c := &PluginController{Store: store, Git: git}
		p := newPlugin(4, &v1alpha1.Repository{URL: "https://github.com/o/r", Branch: "--upload-pack=touch /tmp/pwn"})
		_, _, err := c.reconcile(context.Background(), p)
		if err == nil {
			t.Fatal("retryable failure must return a non-nil error (requeue)")
		}
		got := store.plugin(t, ns, name, tag)
		if got.Status.ObservedGeneration != 0 {
			t.Errorf("retryable must NOT bump observedGeneration, got %d", got.Status.ObservedGeneration)
		}
		if r := readyReason(got); r != "SourceUnresolvable" {
			t.Errorf("ready reason = %q, want SourceUnresolvable", r)
		}
	})

	t.Run("oci source is terminal SourceUnsupported", func(t *testing.T) {
		store := newFakePluginStore()
		c := &PluginController{Store: store, Git: git}
		p := newPlugin(5, nil)
		p.Spec.Source = &v1alpha1.PluginSource{
			Type: v1alpha1.PluginSourceTypeOCI,
			OCI:  &v1alpha1.PluginSourceOCI{Reference: "ghcr.io/o/p@sha256:abc"},
		}
		outcome, reason, err := c.reconcile(context.Background(), p)
		if err != nil {
			t.Fatalf("terminal must Forget, got %v", err)
		}
		if outcome != "failed" || reason != "SourceUnsupported" {
			t.Fatalf("got (%q, %q), want (failed, SourceUnsupported)", outcome, reason)
		}
		if got := store.plugin(t, ns, name, tag); got.Status.ObservedGeneration != 5 {
			t.Errorf("observedGeneration = %d, want 5", got.Status.ObservedGeneration)
		}
	})
}
