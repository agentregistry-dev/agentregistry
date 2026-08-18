package gitutil

import (
	"context"
	"fmt"
	"net/url"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// PinRef resolves a repository's ref — an explicit commit, else the branch, else
// the remote's default HEAD — to a concrete commit SHA, so a caller can record
// an immutable pin without fetching a tree. creds may be nil (fetch
// anonymously); a credential-resolution failure is retryable, since a credential
// created after the resource recovers on resync.
func PinRef(ctx context.Context, namespace string, repo *v1alpha1.Repository, creds types.GitCredentialFunc) (string, error) {
	if repo == nil || repo.URL == "" {
		return "", fmt.Errorf("pin git ref: repository url is required")
	}
	auth, err := resolveCredentials(ctx, namespace, repo, creds)
	if err != nil {
		return "", err
	}
	return ResolveRefContext(ctx, repo.URL, pinRef(repo), auth)
}

// PinAndCopy pins a repository's ref (see PinRef) and copies the tree at that
// commit — repo.Subfolder when set — into targetDir, returning the pinned
// commit. Credentials are resolved once for both git invocations.
func PinAndCopy(ctx context.Context, namespace string, repo *v1alpha1.Repository, targetDir string, creds types.GitCredentialFunc) (string, error) {
	if repo == nil || repo.URL == "" {
		return "", fmt.Errorf("clone git source: repository url is required")
	}
	auth, err := resolveCredentials(ctx, namespace, repo, creds)
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

// pinRef prefers an explicit commit over the branch; empty means "the remote's
// default HEAD".
func pinRef(repo *v1alpha1.Repository) string {
	if repo.Commit != "" {
		return repo.Commit
	}
	return repo.Branch
}

// resolveCredentials looks up credentials for repo. A nil creds hook (OSS) or a
// nil result means fetch anonymously.
func resolveCredentials(ctx context.Context, namespace string, repo *v1alpha1.Repository, creds types.GitCredentialFunc) (*url.Userinfo, error) {
	if creds == nil {
		return nil, nil
	}
	auth, err := creds(ctx, namespace, repo)
	if err != nil {
		return nil, fmt.Errorf("resolve git credentials: %w", err)
	}
	return auth, nil
}
