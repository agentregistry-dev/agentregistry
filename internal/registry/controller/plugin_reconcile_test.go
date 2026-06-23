package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/source"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// fakePluginStore captures status patches by replaying the raw-JSON callback,
// so reconcile/patchStatus can be tested with no database.
type fakePluginStore struct {
	status  map[string]json.RawMessage
	reasons []string // Ready-condition reasons, in apply order
}

func newFakePluginStore() *fakePluginStore {
	return &fakePluginStore{status: map[string]json.RawMessage{}}
}

func (f *fakePluginStore) key(ns, name, tag string) string { return ns + "/" + name + ":" + tag }

func (f *fakePluginStore) Get(context.Context, string, string, string) (*v1alpha1.RawObject, error) {
	return nil, pkgdb.ErrNotFound
}

func (f *fakePluginStore) List(context.Context, v1alpha1store.ListOpts) ([]*v1alpha1.RawObject, string, error) {
	return nil, "", nil
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

type fakeResolver struct {
	resolved *v1alpha1.PluginResolvedSource
	bundle   *bundle.CanonicalBundle
	err      error
}

func (r fakeResolver) Resolve(context.Context, *v1alpha1.Plugin) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error) {
	return r.resolved, r.bundle, r.err
}

func readyReason(p *v1alpha1.Plugin) string {
	if c := p.Status.GetCondition(pluginReadyCondition); c != nil {
		return c.Reason
	}
	return ""
}

func TestPluginReconcile(t *testing.T) {
	const ns, name, tag = "default", "p", "v1"
	newPlugin := func(gen int64) *v1alpha1.Plugin {
		p := &v1alpha1.Plugin{Metadata: v1alpha1.ObjectMeta{Namespace: ns, Name: name, Tag: tag, Generation: gen}}
		p.Spec.Origin = &v1alpha1.PluginOrigin{
			Type: v1alpha1.PluginOriginTypeGit,
			Git:  &v1alpha1.PluginOriginGit{Repository: &v1alpha1.Repository{URL: "https://github.com/o/r", Branch: "main"}},
		}
		return p
	}
	goodBundle := &bundle.CanonicalBundle{Files: map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"p"}`),
		"skills/x/SKILL.md":          []byte("---\nname: x\n---\n"),
	}}

	t.Run("success transitions Progressing then Resolved and bumps observedGeneration", func(t *testing.T) {
		store := newFakePluginStore()
		c := &PluginController{Store: store, Resolver: fakeResolver{
			resolved: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginOriginTypeGit, Commit: "deadbeef"},
			bundle:   goodBundle,
		}}
		outcome, _, err := c.reconcile(context.Background(), newPlugin(2))
		if err != nil || outcome != "resolved" {
			t.Fatalf("reconcile = (%q, %v), want (resolved, nil)", outcome, err)
		}
		if !reflect.DeepEqual(store.reasons, []string{"Progressing", "Resolved"}) {
			t.Fatalf("reason sequence = %v, want [Progressing Resolved]", store.reasons)
		}
		got := store.plugin(t, ns, name, tag)
		if !got.Status.IsConditionTrue(pluginReadyCondition) {
			t.Error("expected Ready=True")
		}
		if got.Status.ObservedGeneration != 2 {
			t.Errorf("observedGeneration = %d, want 2", got.Status.ObservedGeneration)
		}
		if got.Status.ResolvedSource == nil || got.Status.ResolvedSource.Commit != "deadbeef" {
			t.Errorf("resolvedSource = %+v", got.Status.ResolvedSource)
		}
		if got.Status.Manifest == nil || got.Status.Inventory == nil {
			t.Error("expected manifest + inventory populated")
		}
	})

	t.Run("terminal unsupported origin forgets and bumps observedGeneration", func(t *testing.T) {
		store := newFakePluginStore()
		c := &PluginController{Store: store, Resolver: fakeResolver{err: fmt.Errorf("x: %w", source.ErrUnsupportedOrigin)}}
		outcome, reason, err := c.reconcile(context.Background(), newPlugin(3))
		if err != nil {
			t.Fatalf("terminal failure must return nil error (Forget), got %v", err)
		}
		if outcome != "failed" || reason != "OriginUnsupported" {
			t.Fatalf("got (%q, %q), want (failed, OriginUnsupported)", outcome, reason)
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
		c := &PluginController{Store: store, Resolver: fakeResolver{err: errors.New("dial tcp: timeout")}}
		_, _, err := c.reconcile(context.Background(), newPlugin(4))
		if err == nil {
			t.Fatal("retryable failure must return a non-nil error (requeue)")
		}
		got := store.plugin(t, ns, name, tag)
		if got.Status.ObservedGeneration != 0 {
			t.Errorf("retryable must NOT bump observedGeneration, got %d", got.Status.ObservedGeneration)
		}
		if r := readyReason(got); r != "OriginUnresolvable" {
			t.Errorf("ready reason = %q, want OriginUnresolvable", r)
		}
	})

	t.Run("malformed manifest is terminal SourceInvalid", func(t *testing.T) {
		store := newFakePluginStore()
		badBundle := &bundle.CanonicalBundle{Files: map[string][]byte{".claude-plugin/plugin.json": []byte("{bad")}}
		c := &PluginController{Store: store, Resolver: fakeResolver{
			resolved: &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginOriginTypeGit, Commit: "c"},
			bundle:   badBundle,
		}}
		outcome, reason, err := c.reconcile(context.Background(), newPlugin(5))
		if err != nil {
			t.Fatalf("terminal must Forget, got %v", err)
		}
		if outcome != "failed" || reason != "SourceInvalid" {
			t.Fatalf("got (%q, %q), want (failed, SourceInvalid)", outcome, reason)
		}
		if got := store.plugin(t, ns, name, tag); got.Status.ObservedGeneration != 5 {
			t.Errorf("observedGeneration = %d, want 5", got.Status.ObservedGeneration)
		}
	})
}
