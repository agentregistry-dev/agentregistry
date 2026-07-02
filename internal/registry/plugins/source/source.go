// Package source resolves a Plugin's pinned source pointer into a concrete
// commit (git) or digest (oci) and loads the bundle files at that pin. The
// registry does NOT host plugin bundles — this package is how the controller
// (at resolve time) and deploys (at materialize time) turn a Plugin.Spec.Source
// into an in-memory bundle.CanonicalBundle.
package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// cloneTimeout bounds a single resolve (ls-remote + shallow clone) so a slow or
// hostile origin cannot hang the controller worker indefinitely.
const cloneTimeout = 2 * time.Minute

var (
	// ErrUnsupportedSource marks a source the resolver cannot handle — a
	// TERMINAL condition (retrying will not help). OCI sources and non-GitHub
	// git hosts are currently unsupported.
	ErrUnsupportedSource = errors.New("source: unsupported plugin source")
	// ErrSourceNotFound marks a ref that resolves to no commit on the remote
	// (deleted/typo'd branch or tag, or a non-existent SHA) — TERMINAL.
	ErrSourceNotFound = errors.New("source: git ref not found")
)

// Resolver pins a plugin's source and loads its bundle. Transient failures
// (network, clone) are returned as plain errors (retryable); permanent
// rejections wrap ErrUnsupportedSource, and malformed bundle content wraps
// bundle.ErrInvalidBundle — both terminal.
type Resolver interface {
	Resolve(ctx context.Context, p *v1alpha1.Plugin) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error)
}

// GitResolver resolves git sources: it resolves the ref to a commit SHA via
// `git ls-remote` (no clone) and then shallow-clones that exact commit. It
// shells out to system git with ambient credentials, and only github.com is
// supported today (matching existing skill/agent source behavior). OCI sources
// are not yet implemented.
type GitResolver struct{}

// NewGitResolver returns a git-backed Resolver.
func NewGitResolver() *GitResolver { return &GitResolver{} }

func (r *GitResolver) Resolve(ctx context.Context, p *v1alpha1.Plugin) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error) {
	if p == nil || p.Spec.Source == nil {
		return nil, nil, fmt.Errorf("%w: plugin has no source", ErrUnsupportedSource)
	}
	o := p.Spec.Source
	switch o.Type {
	case v1alpha1.PluginSourceTypeGit:
		return r.resolveGit(ctx, o.Git)
	case v1alpha1.PluginSourceTypeOCI:
		return nil, nil, fmt.Errorf("%w: oci plugin source not yet supported (use a git source)", ErrUnsupportedSource)
	default:
		return nil, nil, fmt.Errorf("%w: unknown plugin source type %q", ErrUnsupportedSource, o.Type)
	}
}

func (r *GitResolver) resolveGit(ctx context.Context, g *v1alpha1.PluginSourceGit) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error) {
	if g == nil || g.Repository == nil || g.Repository.URL == "" {
		return nil, nil, fmt.Errorf("%w: git source missing repository url", ErrUnsupportedSource)
	}
	repo := g.Repository

	// Bound the whole resolve (ls-remote + clone) so a slow/hostile origin can't
	// hang the worker. gitutil kills the git child when ctx expires.
	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()

	// Prefer an explicit commit; otherwise resolve the branch/tag (or the
	// remote default HEAD) to a concrete SHA so status records an immutable pin.
	ref := repo.Commit
	if ref == "" {
		ref = repo.Branch
	}
	commit, err := gitutil.ResolveRefContext(ctx, repo.URL, ref)
	if err != nil {
		return nil, nil, classifyGitErr(err, "resolve git ref "+ref)
	}

	dir, err := os.MkdirTemp("", "arctl-plugin-src-*")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// branch="" + commit=resolved => clone the default branch shallow, then
	// fetch+checkout the exact pinned commit (gitutil does the fetch-by-SHA).
	if err := gitutil.CloneAndCopyContext(ctx, repo.URL, "", commit, repo.Subfolder, dir, false); err != nil {
		return nil, nil, classifyGitErr(err, "clone git source")
	}

	b, err := bundle.FromDir(dir)
	if err != nil {
		return nil, nil, err // wraps bundle.ErrInvalidBundle (terminal)
	}
	return &v1alpha1.PluginResolvedSource{Type: v1alpha1.PluginSourceTypeGit, Commit: commit}, b, nil
}

// classifyGitErr maps a gitutil error to the resolver's terminal/retryable
// contract: a non-github host or a missing ref is terminal (wrapped in a
// terminal sentinel); anything else (network, transport) is retryable.
func classifyGitErr(err error, context string) error {
	switch {
	case errors.Is(err, gitutil.ErrUnsupportedHost):
		return fmt.Errorf("%w: %v", ErrUnsupportedSource, err)
	case errors.Is(err, gitutil.ErrRefNotFound):
		return fmt.Errorf("%w: %v", ErrSourceNotFound, err)
	default:
		return fmt.Errorf("%s: %w", context, err) // retryable
	}
}
