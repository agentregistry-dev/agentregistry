package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// BundleSource yields the raw bundle tar bytes for a plugin being published.
// The first implementation pulls from a pinned OCI origin; a git fetcher is a
// future drop-in that implements the same interface with no other changes.
type BundleSource interface {
	Fetch(ctx context.Context, p *v1alpha1.Plugin) ([]byte, error)
}

// OCIOriginFetcher resolves a plugin's pinned OCI origin into raw bundle tar
// bytes by pulling the artifact and extracting its single layer. Git origins
// are not yet supported (no git client is vendored) and fail with a clear
// error rather than silently.
type OCIOriginFetcher struct {
	keychain authn.Keychain
	insecure bool
	timeout  time.Duration
}

// NewOCIOriginFetcher builds a fetcher. A nil keychain defaults to
// authn.DefaultKeychain. insecure permits plain-HTTP origins (local/test).
func NewOCIOriginFetcher(keychain authn.Keychain, insecure bool) *OCIOriginFetcher {
	if keychain == nil {
		keychain = authn.DefaultKeychain
	}
	return &OCIOriginFetcher{keychain: keychain, insecure: insecure, timeout: 60 * time.Second}
}

func (f *OCIOriginFetcher) Fetch(ctx context.Context, p *v1alpha1.Plugin) ([]byte, error) {
	o := p.Spec.Origin
	if o == nil {
		return nil, fmt.Errorf("%w: plugin has no origin", ErrInvalidBundle)
	}
	switch o.Type {
	case v1alpha1.PluginOriginTypeOCI:
		if o.OCI == nil || o.OCI.Reference == "" {
			return nil, fmt.Errorf("%w: oci origin missing reference", ErrInvalidBundle)
		}
		var nopts []gname.Option
		if f.insecure {
			nopts = append(nopts, gname.Insecure)
		}
		ref, err := gname.NewDigest(o.OCI.Reference, nopts...)
		if err != nil {
			return nil, fmt.Errorf("%w: origin %v", ErrNotDigestPinned, err)
		}
		cctx, cancel := context.WithTimeout(ctx, f.timeout)
		defer cancel()
		img, err := remote.Image(ref, remote.WithAuthFromKeychain(f.keychain), remote.WithContext(cctx))
		if err != nil {
			return nil, fmt.Errorf("%w: pull origin %s: %v", ErrPull, o.OCI.Reference, err)
		}
		// First cut assumes a single-layer plugin artifact (the shape this
		// package produces). Multi-layer/image flattening is a future addition.
		return artifactTarBytes(img)
	case v1alpha1.PluginOriginTypeGit:
		return nil, fmt.Errorf("%w: git plugin origin not yet supported (publish via an OCI origin)", ErrInvalidBundle)
	default:
		return nil, fmt.Errorf("%w: unknown plugin origin type %q", ErrInvalidBundle, o.Type)
	}
}
