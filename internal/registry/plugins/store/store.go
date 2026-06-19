package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	gname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// Store persists canonical plugin bundles as OCI artifacts and reads them back.
type Store interface {
	// Push serializes the bundle to its deterministic tar, computes the content
	// hash, builds an OCI artifact, and writes it to the configured registry
	// under <prefix>/<namespace>/<name>:<tag>. It returns the digest-pinned
	// reference (…@sha256:<manifestDigest>) and the bundle content hash (sha256
	// hex of the canonical tar — NOT the manifest digest).
	Push(ctx context.Context, namespace, name, tag string, b *CanonicalBundle) (ociRef string, contentHash string, err error)

	// Pull fetches the artifact at a digest-pinned ref and reconstructs the
	// CanonicalBundle. A non-digest ref is rejected (ErrNotDigestPinned).
	Pull(ctx context.Context, ref string) (*CanonicalBundle, error)
}

// Config selects the OCI registry canonical bundles are stored in and how to
// authenticate. Registry is required; an empty Registry must make the caller
// fail closed rather than persist un-storable plugins.
type Config struct {
	// Registry is the OCI registry host, e.g. "ghcr.io" or "localhost:5001".
	Registry string
	// RepositoryPrefix is prepended to the per-plugin repo path, e.g.
	// "agentregistry/plugins". Final repo: <Registry>/<prefix>/<namespace>/<name>.
	RepositoryPrefix string
	// Keychain supplies push/pull credentials. Defaults to
	// authn.DefaultKeychain when nil. In-cluster deployments that push to a
	// cloud registry must compose the appropriate keychain provider explicitly.
	Keychain authn.Keychain
	// Insecure allows plain-HTTP registries (local dev / in-memory test).
	Insecure bool
	// Timeout bounds a single push or pull. Defaults to 60s when zero.
	Timeout time.Duration
}

type ociStore struct {
	cfg Config
}

// NewOCIStore validates cfg and returns an OCI-backed Store.
func NewOCIStore(cfg Config) (Store, error) {
	if strings.TrimSpace(cfg.Registry) == "" {
		return nil, errors.New("store: Registry is required")
	}
	if cfg.Keychain == nil {
		cfg.Keychain = authn.DefaultKeychain
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 60 * time.Second
	}
	return &ociStore{cfg: cfg}, nil
}

func (s *ociStore) repo(namespace, name string) string {
	parts := []string{s.cfg.Registry}
	if s.cfg.RepositoryPrefix != "" {
		parts = append(parts, strings.Trim(s.cfg.RepositoryPrefix, "/"))
	}
	return strings.Join(append(parts, namespace, name), "/")
}

func (s *ociStore) nameOpts() []gname.Option {
	if s.cfg.Insecure {
		return []gname.Option{gname.Insecure}
	}
	return nil
}

func (s *ociStore) Push(ctx context.Context, namespace, name, tag string, b *CanonicalBundle) (string, string, error) {
	tarBytes, err := b.Bytes()
	if err != nil {
		return "", "", err
	}
	contentHash := sha256hex(tarBytes)

	img, err := buildArtifact(tarBytes, map[string]string{
		"dev.agentregistry.plugin.contentHash": contentHash,
		"dev.agentregistry.plugin.name":        name,
	})
	if err != nil {
		return "", "", err
	}

	repo := s.repo(namespace, name)
	tagRef, err := gname.ParseReference(repo+":"+tag, s.nameOpts()...)
	if err != nil {
		return "", "", fmt.Errorf("%w: parse %q: %v", ErrPush, repo+":"+tag, err)
	}

	cctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	if err := remote.Write(tagRef, img, remote.WithAuthFromKeychain(s.cfg.Keychain), remote.WithContext(cctx)); err != nil {
		return "", "", fmt.Errorf("%w: %v", ErrPush, err)
	}

	dig, err := img.Digest()
	if err != nil {
		return "", "", fmt.Errorf("%w: digest: %v", ErrPush, err)
	}
	return repo + "@" + dig.String(), contentHash, nil
}

func (s *ociStore) Pull(ctx context.Context, ref string) (*CanonicalBundle, error) {
	digRef, err := gname.NewDigest(ref, s.nameOpts()...)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotDigestPinned, err)
	}
	cctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	img, err := remote.Image(digRef, remote.WithAuthFromKeychain(s.cfg.Keychain), remote.WithContext(cctx))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPull, err)
	}
	tarBytes, err := artifactTarBytes(img)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPull, err)
	}
	return FromTar(tarBytes)
}
