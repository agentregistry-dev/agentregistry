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
	"github.com/agentregistry-dev/agentregistry/pkg/types"
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

// Resolve pins a plugin's source and loads its bundle at that pin. Transient
// failures (network, clone, credential lookup) are returned as plain errors
// (retryable); permanent rejections wrap ErrUnsupportedSource, and malformed
// bundle content wraps bundle.ErrInvalidBundle — both terminal.
//
// It shells out to system git, and only github.com is supported today (matching
// existing skill/agent source behavior). OCI sources are not yet implemented.
// creds resolves credentials for a private repository; nil fetches anonymously.
func Resolve(ctx context.Context, p *v1alpha1.Plugin, creds types.GitCredentialFunc) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error) {
	if p == nil || p.Spec.Source == nil {
		return nil, nil, fmt.Errorf("%w: plugin has no source", ErrUnsupportedSource)
	}
	o := p.Spec.Source
	switch o.Type {
	case v1alpha1.PluginSourceTypeGit:
		return resolveGit(ctx, p.Metadata.NamespaceOrDefault(), o.Git, creds)
	case v1alpha1.PluginSourceTypeOCI:
		return nil, nil, fmt.Errorf("%w: oci plugin source not yet supported (use a git source)", ErrUnsupportedSource)
	default:
		return nil, nil, fmt.Errorf("%w: unknown plugin source type %q", ErrUnsupportedSource, o.Type)
	}
}

func resolveGit(ctx context.Context, namespace string, g *v1alpha1.PluginSourceGit, creds types.GitCredentialFunc) (*v1alpha1.PluginResolvedSource, *bundle.CanonicalBundle, error) {
	if g == nil || g.Repository == nil || g.Repository.URL == "" {
		return nil, nil, fmt.Errorf("%w: git source missing repository url", ErrUnsupportedSource)
	}
	repo := g.Repository

	// Bound the whole resolve (ls-remote + clone) so a slow/hostile origin can't
	// hang the worker. gitutil kills the git child when ctx expires.
	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()

	dir, err := os.MkdirTemp("", "arctl-plugin-src-*")
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// Pin the ref to a concrete SHA so status records an immutable pointer, and
	// copy the tree at that pin for bundle loading.
	commit, err := gitutil.PinAndCopy(ctx, namespace, repo, dir, creds)
	if err != nil {
		return nil, nil, classifyGitErr(err, "resolve git source")
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
