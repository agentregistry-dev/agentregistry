// Package source resolves a Plugin's pinned origin pointer into a concrete
// commit (git) or digest (oci) and loads the bundle files at that pin. The
// registry does NOT host plugin bundles — this package is how the controller
// (at resolve time) and deploys (at materialize time) turn a Plugin.Spec.Origin
// into an in-memory bundle.CanonicalBundle.
package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ErrUnsupportedOrigin marks an origin the resolver cannot handle — a TERMINAL
// condition (retrying will not help). OCI origins and non-GitHub git hosts are
// currently unsupported.
var ErrUnsupportedOrigin = errors.New("source: unsupported plugin origin")

// Resolver pins a plugin's origin and loads its bundle. Transient failures
// (network, clone) are returned as plain errors (retryable); permanent
// rejections wrap ErrUnsupportedOrigin, and malformed bundle content wraps
// bundle.ErrInvalidBundle — both terminal.
type Resolver interface {
	Resolve(ctx context.Context, p *v1alpha1.Plugin) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error)
}

// GitResolver resolves git origins: it resolves the ref to a commit SHA via
// `git ls-remote` (no clone) and then shallow-clones that exact commit. It
// shells out to system git with ambient credentials, and only github.com is
// supported today (matching existing skill/agent source behavior). OCI origins
// are not yet implemented.
type GitResolver struct{}

// NewGitResolver returns a git-backed Resolver.
func NewGitResolver() *GitResolver { return &GitResolver{} }

func (r *GitResolver) Resolve(ctx context.Context, p *v1alpha1.Plugin) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error) {
	if p == nil || p.Spec.Origin == nil {
		return nil, nil, fmt.Errorf("%w: plugin has no origin", ErrUnsupportedOrigin)
	}
	o := p.Spec.Origin
	switch o.Type {
	case v1alpha1.PluginOriginTypeGit:
		return r.resolveGit(ctx, o.Git)
	case v1alpha1.PluginOriginTypeOCI:
		return nil, nil, fmt.Errorf("%w: oci plugin origin not yet supported (use a git origin)", ErrUnsupportedOrigin)
	default:
		return nil, nil, fmt.Errorf("%w: unknown plugin origin type %q", ErrUnsupportedOrigin, o.Type)
	}
}

func (r *GitResolver) resolveGit(_ context.Context, g *v1alpha1.PluginOriginGit) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error) {
	if g == nil || g.Repository == nil || g.Repository.URL == "" {
		return nil, nil, fmt.Errorf("%w: git origin missing repository url", ErrUnsupportedOrigin)
	}
	repo := g.Repository

	// Prefer an explicit commit; otherwise resolve the branch/tag (or the
	// remote default HEAD) to a concrete SHA so status records an immutable pin.
	ref := repo.Commit
	if ref == "" {
		ref = repo.Branch
	}
	commit, err := gitutil.ResolveRef(repo.URL, ref)
	if err != nil {
		if isUnsupportedHost(err) {
			return nil, nil, fmt.Errorf("%w: %v", ErrUnsupportedOrigin, err)
		}
		return nil, nil, fmt.Errorf("resolve git ref %q: %w", ref, err) // retryable
	}

	dir, err := os.MkdirTemp("", "arctl-plugin-src-*")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// branch="" + commit=resolved => clone the default branch shallow, then
	// fetch+checkout the exact pinned commit (gitutil does the fetch-by-SHA).
	if err := gitutil.CloneAndCopy(repo.URL, "", commit, repo.Subfolder, dir, false); err != nil {
		if isUnsupportedHost(err) {
			return nil, nil, fmt.Errorf("%w: %v", ErrUnsupportedOrigin, err)
		}
		return nil, nil, fmt.Errorf("clone git source: %w", err) // retryable
	}

	b, err := bundle.FromDir(dir)
	if err != nil {
		return nil, nil, err // wraps bundle.ErrInvalidBundle (terminal)
	}
	return &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginOriginTypeGit, Commit: commit}, b, nil
}

// isUnsupportedHost reports whether err is gitutil's non-github.com rejection,
// which is permanent (terminal) rather than a transient network failure.
func isUnsupportedHost(err error) bool {
	return err != nil && strings.Contains(err.Error(), "only github.com is supported")
}
