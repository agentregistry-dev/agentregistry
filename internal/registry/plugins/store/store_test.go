package store

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// anonKeychain is a hermetic anonymous keychain for tests (avoids reading
// ~/.docker/config.json that authn.DefaultKeychain would).
type anonKeychain struct{}

func (anonKeychain) Resolve(authn.Resource) (authn.Authenticator, error) {
	return authn.Anonymous, nil
}

func newTestStore(t *testing.T) (Store, string) {
	t.Helper()
	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)
	host := strings.TrimPrefix(srv.URL, "http://")
	st, err := NewOCIStore(Config{
		Registry:         host,
		RepositoryPrefix: "agentregistry/plugins",
		Insecure:         true,
		Keychain:         anonKeychain{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return st, host
}

func TestStorePushPullRoundTrip(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	want := sampleBundle()

	ociRef, contentHash, err := st.Push(ctx, "default", "my-plugin", "v1", want)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if !strings.Contains(ociRef, "@sha256:") {
		t.Fatalf("ociRef not digest-pinned: %q", ociRef)
	}
	wantHash, _ := want.ContentHash()
	if contentHash != wantHash {
		t.Fatalf("contentHash %q != bundle hash %q", contentHash, wantHash)
	}

	got, err := st.Pull(ctx, ociRef)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !reflect.DeepEqual(want.Files, got.Files) {
		t.Fatalf("round-trip mismatch:\n want %v\n got  %v", want.Files, got.Files)
	}
}

func TestStorePushIsDeterministic(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	r1, h1, err := st.Push(ctx, "default", "p", "v1", sampleBundle())
	if err != nil {
		t.Fatal(err)
	}
	r2, h2, err := st.Push(ctx, "default", "p", "v1", sampleBundle())
	if err != nil {
		t.Fatal(err)
	}
	if r1 != r2 || h1 != h2 {
		t.Fatalf("non-deterministic push: ref %q/%q hash %q/%q", r1, r2, h1, h2)
	}
}

func TestStorePullRejectsNonDigestRef(t *testing.T) {
	st, host := newTestStore(t)
	tagRef := host + "/agentregistry/plugins/default/my-plugin:v1"
	if _, err := st.Pull(context.Background(), tagRef); !errors.Is(err, ErrNotDigestPinned) {
		t.Fatalf("expected ErrNotDigestPinned for tag ref, got %v", err)
	}
}

func TestStoreArtifactType(t *testing.T) {
	st, _ := newTestStore(t)
	ctx := context.Background()
	ociRef, _, err := st.Push(ctx, "default", "my-plugin", "v1", sampleBundle())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := gname.NewDigest(ociRef, gname.Insecure)
	if err != nil {
		t.Fatal(err)
	}
	desc, err := remote.Get(ref, remote.WithAuthFromKeychain(anonKeychain{}))
	if err != nil {
		t.Fatal(err)
	}
	if string(desc.ArtifactType) != ArtifactType {
		t.Fatalf("artifactType = %q, want %q", desc.ArtifactType, ArtifactType)
	}
}

func TestNewOCIStoreRequiresRegistry(t *testing.T) {
	if _, err := NewOCIStore(Config{}); err == nil {
		t.Fatal("expected error for empty Registry")
	}
}
