package gitutil

import (
	"context"
	"fmt"
	"net/url"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// Source pins and fetches git sources on behalf of the registry, resolving
// per-repository credentials through a single hook. One Source is built at the
// composition root and shared by every consumer (the Plugin and Skill
// controllers today), so credentials enter the system in exactly one place.
type Source struct {
	credentials types.GitCredentialFunc
}

// NewSource returns a Source that authenticates through credentials. A nil hook
// (the OSS default) fetches anonymously, so private repositories fail.
func NewSource(credentials types.GitCredentialFunc) *Source {
	return &Source{credentials: credentials}
}

// Pin resolves a repository's ref — an explicit commit, else the branch, else
// the remote's default HEAD — to a concrete commit SHA, so a caller can record
// an immutable pin without fetching a tree.
func (s *Source) Pin(ctx context.Context, namespace string, repo *v1alpha1.Repository) (string, error) {
	if repo == nil || repo.URL == "" {
		return "", fmt.Errorf("pin git ref: repository url is required")
	}
	auth, err := s.auth(ctx, namespace, repo)
	if err != nil {
		return "", err
	}
	return ResolveRefContext(ctx, repo.URL, pinRef(repo), auth)
}

// Fetch pins a repository's ref (see Pin) and copies the tree at that commit —
// repo.Subfolder when set — into targetDir, returning the pinned commit.
// Credentials are resolved once for both git invocations.
func (s *Source) Fetch(ctx context.Context, namespace string, repo *v1alpha1.Repository, targetDir string) (string, error) {
	if repo == nil || repo.URL == "" {
		return "", fmt.Errorf("fetch git source: repository url is required")
	}
	auth, err := s.auth(ctx, namespace, repo)
	if err != nil {
		return "", err
	}
	commit, err := ResolveRefContext(ctx, repo.URL, pinRef(repo), auth)
	if err != nil {
		return "", err
	}
	// branch="" + commit=resolved => shallow-clone the default branch, then
	// fetch+checkout the exact pinned commit (CloneAndCopyContext fetches by SHA).
	if err := CloneAndCopyContext(ctx, repo.URL, "", commit, repo.Subfolder, targetDir, false, auth); err != nil {
		return "", err
	}
	return commit, nil
}

// auth looks up credentials for repo. A nil hook (OSS) or a nil result means
// fetch anonymously. A lookup failure is retryable: a credential created after
// the resource recovers on the next resync.
func (s *Source) auth(ctx context.Context, namespace string, repo *v1alpha1.Repository) (*url.Userinfo, error) {
	if s == nil || s.credentials == nil {
		return nil, nil
	}
	auth, err := s.credentials(ctx, namespace, repo)
	if err != nil {
		return nil, fmt.Errorf("resolve git credentials: %w", err)
	}
	return auth, nil
}

// pinRef prefers an explicit commit over the branch; empty means "the remote's
// default HEAD".
func pinRef(repo *v1alpha1.Repository) string {
	if repo.Commit != "" {
		return repo.Commit
	}
	return repo.Branch
}
