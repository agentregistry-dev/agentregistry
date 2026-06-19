package store

import (
	"context"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func ociOriginPlugin(ref string) *v1alpha1.Plugin {
	return &v1alpha1.Plugin{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindPlugin},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "my-plugin", Tag: "v1"},
		Spec: v1alpha1.PluginSpec{
			Origin: &v1alpha1.PluginOrigin{Type: v1alpha1.PluginOriginTypeOCI, OCI: &v1alpha1.PluginOriginOCI{Reference: ref}},
		},
	}
}

func TestPluginPrepareHappyPath(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()

	// Seed an "origin" artifact: the author's published bundle in the registry.
	originRef, _, err := st.Push(ctx, "author", "plugin", "v1", sampleBundle())
	if err != nil {
		t.Fatalf("seed origin: %v", err)
	}

	fetcher := NewOCIOriginFetcher(anonKeychain{}, true)
	prep := NewPluginPrepare(fetcher, st)
	p := ociOriginPlugin(originRef)

	if err := prep(ctx, p); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if p.Spec.Content == nil {
		t.Fatal("spec.Content not populated")
	}
	if !strings.Contains(p.Spec.Content.OCIRef, "@sha256:") {
		t.Fatalf("Content.OCIRef not digest-pinned: %q", p.Spec.Content.OCIRef)
	}
	wantHash, _ := sampleBundle().ContentHash()
	if p.Spec.Content.ContentHash != wantHash {
		t.Fatalf("Content.ContentHash %q != %q", p.Spec.Content.ContentHash, wantHash)
	}
	if p.Spec.Manifest == nil || len(p.Spec.Manifest.Skills) == 0 {
		t.Fatalf("manifest not indexed: %+v", p.Spec.Manifest)
	}
}

func TestPluginPrepareFailsClosed(t *testing.T) {
	err := NewPluginPrepare(nil, nil)(context.Background(), ociOriginPlugin("ghcr.io/x/y@sha256:"+strings.Repeat("a", 64)))
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected fail-closed error, got %v", err)
	}
}

func TestOCIOriginFetcherGitUnsupported(t *testing.T) {
	f := NewOCIOriginFetcher(anonKeychain{}, true)
	p := &v1alpha1.Plugin{Spec: v1alpha1.PluginSpec{Origin: &v1alpha1.PluginOrigin{
		Type: v1alpha1.PluginOriginTypeGit,
		Git:  &v1alpha1.PluginOriginGit{Repository: &v1alpha1.Repository{URL: "https://github.com/x/y", Commit: "abc"}},
	}}}
	if _, err := f.Fetch(context.Background(), p); err == nil || !strings.Contains(err.Error(), "not yet supported") {
		t.Fatalf("expected git-unsupported error, got %v", err)
	}
}
