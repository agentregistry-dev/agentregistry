package gitutil

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	githttp "github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/storage/memory"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// Source pins and fetches git sources for the registry.
type Source interface {
	// Pin resolves repo's commit, branch, or HEAD to a SHA without fetching.
	Pin(ctx context.Context, namespace string, repo *v1alpha1.Repository) (string, error)
	// Fetch writes repo's tree at the pinned commit into dest and returns the SHA.
	Fetch(ctx context.Context, namespace string, repo *v1alpha1.Repository, dest string) (string, error)
}

// Limits caps what one Fetch writes from an untrusted repository.
type Limits struct {
	MaxBytes   int64
	MaxEntries int
}

type source struct {
	credentials types.GitCredentialFunc
	limits      Limits
}

// NewSource returns a Source; nil credentials fetch anonymously.
func NewSource(credentials types.GitCredentialFunc, limits Limits) Source {
	return &source{credentials: credentials, limits: limits}
}

func (s *source) Pin(ctx context.Context, namespace string, repo *v1alpha1.Repository) (string, error) {
	cloneURL, ref, _, auth, err := s.remote(ctx, namespace, repo)
	if err != nil {
		return "", err
	}
	return resolveRef(ctx, cloneURL, ref, auth)
}

func (s *source) Fetch(ctx context.Context, namespace string, repo *v1alpha1.Repository, dest string) (string, error) {
	cloneURL, ref, subPath, auth, err := s.remote(ctx, namespace, repo)
	if err != nil {
		return "", err
	}
	sha, err := resolveRef(ctx, cloneURL, ref, auth)
	if err != nil {
		return "", err
	}
	scratch, err := os.MkdirTemp("", "gitutil-*")
	if err != nil {
		return "", err
	}
	defer func() { _ = os.RemoveAll(scratch) }()
	tree, err := fetchTree(ctx, scratch, cloneURL, sha, auth)
	if err != nil {
		return "", err
	}
	if tree, err = subtree(tree, subPath); err != nil {
		return "", err
	}
	return sha, writeTree(tree, dest, s.limits)
}

// remote parses repo into its clone URL, ref, subpath, and credentials.
func (s *source) remote(ctx context.Context, namespace string, repo *v1alpha1.Repository) (cloneURL, ref, subPath string, auth *url.Userinfo, err error) {
	if repo == nil || repo.URL == "" {
		return "", "", "", nil, errors.New("git source: repository url is required")
	}
	cloneURL, branch, subPath, err := ParseGitURL(repo.URL)
	if err != nil {
		return "", "", "", nil, fmt.Errorf("parse Git URL: %w", err)
	}
	if s.credentials != nil {
		if auth, err = s.credentials(ctx, namespace, repo); err != nil {
			return "", "", "", nil, fmt.Errorf("resolve git credentials: %w", err)
		}
	}
	return cloneURL, cmp.Or(repo.Commit, repo.Branch, branch), cmp.Or(repo.Subfolder, subPath), auth, nil
}

// resolveRef matches a SHA, HEAD, exact refs/ name, tag, then branch, in that order.
func resolveRef(ctx context.Context, cloneURL, ref string, auth *url.Userinfo) (string, error) {
	if plumbing.IsHash(ref) {
		return strings.ToLower(ref), nil
	}
	candidates := []string{"refs/tags/" + ref + "^{}", "refs/tags/" + ref, "refs/heads/" + ref}
	switch {
	case ref == "":
		candidates = []string{"HEAD"}
	case strings.HasPrefix(ref, "refs/"):
		candidates = []string{ref + "^{}", ref}
	}
	remote := git.NewRemote(memory.NewStorage(), &config.RemoteConfig{Name: git.DefaultRemoteName, URLs: []string{cloneURL}})
	refs, err := remote.ListContext(ctx, &git.ListOptions{Auth: basicAuth(auth), PeelingOption: git.AppendPeeled})
	if err != nil && !errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return "", redact(fmt.Errorf("list refs of %s: %w", cloneURL, err), auth)
	}
	byName := make(map[string]*plumbing.Reference, len(refs))
	for _, r := range refs {
		byName[r.Name().String()] = r
	}
	for _, name := range candidates {
		r := byName[name]
		if r != nil && r.Type() == plumbing.SymbolicReference {
			r = byName[r.Target().String()]
		}
		if r != nil {
			return r.Hash().String(), nil
		}
	}
	return "", fmt.Errorf("%w: %q in %s", ErrRefNotFound, cmp.Or(ref, "HEAD"), cloneURL)
}

// fetchTree shallow-fetches sha by hash, which the server must allow (GitHub and GitLab do).
func fetchTree(ctx context.Context, dir, cloneURL, sha string, auth *url.Userinfo) (*object.Tree, error) {
	repo, err := git.PlainInit(dir, true)
	if err != nil {
		return nil, err
	}
	remote := git.NewRemote(repo.Storer, &config.RemoteConfig{Name: git.DefaultRemoteName, URLs: []string{cloneURL}})
	err = remote.FetchContext(ctx, &git.FetchOptions{
		RefSpecs: []config.RefSpec{config.RefSpec(sha + ":refs/heads/fetched")},
		Depth:    1,
		Tags:     git.NoTags,
		Auth:     basicAuth(auth),
	})
	if err != nil {
		return nil, redact(fmt.Errorf("fetch %s from %s: %w", sha, cloneURL, err), auth)
	}
	commit, err := repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return nil, err
	}
	return commit.Tree()
}

// subtree returns the tree at subPath, narrowed to one entry for a file (GitLab /-/blob/ URLs).
func subtree(tree *object.Tree, subPath string) (*object.Tree, error) {
	if subPath == "" {
		return tree, nil
	}
	if !filepath.IsLocal(subPath) {
		return nil, fmt.Errorf("subpath %q escapes repository", subPath)
	}
	clean := path.Clean(subPath)
	entry, err := tree.FindEntry(clean)
	if err != nil {
		return nil, fmt.Errorf("subdirectory %q not found in repository", subPath)
	}
	switch entry.Mode {
	case filemode.Dir:
		return tree.Tree(clean)
	case filemode.Regular, filemode.Executable, filemode.Deprecated:
		parent := tree
		if dir := path.Dir(clean); dir != "." {
			if parent, err = tree.Tree(dir); err != nil {
				return nil, err
			}
		}
		narrowed := *parent
		narrowed.Entries = []object.TreeEntry{*entry}
		return &narrowed, nil
	default:
		return nil, fmt.Errorf("subpath %q is not a directory or regular file", subPath)
	}
}

// writeTree writes tree into dest, keeping writes and symlink targets inside dest.
func writeTree(tree *object.Tree, dest string, limits Limits) error {
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	var entries int
	var size int64
	return tree.Files().ForEach(func(f *object.File) error {
		entries++
		size += f.Size
		if entries > limits.MaxEntries {
			return fmt.Errorf("git source exceeds %d entries", limits.MaxEntries)
		}
		if size > limits.MaxBytes {
			return fmt.Errorf("git source exceeds the size limit of %d bytes", limits.MaxBytes)
		}
		contents, err := f.Contents()
		if err != nil {
			return err
		}
		name := filepath.FromSlash(f.Name)
		if err := root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		switch f.Mode {
		case filemode.Symlink:
			target := filepath.FromSlash(contents)
			if filepath.IsAbs(target) || !filepath.IsLocal(filepath.Join(filepath.Dir(name), target)) {
				return fmt.Errorf("symlink %q target %q escapes the source directory", f.Name, contents)
			}
			return root.Symlink(target, name)
		case filemode.Executable:
			return root.WriteFile(name, []byte(contents), 0o755)
		default:
			return root.WriteFile(name, []byte(contents), 0o644)
		}
	})
}

// basicAuth keeps credentials out of URLs; a token-only userinfo is sent as the username.
func basicAuth(auth *url.Userinfo) transport.AuthMethod {
	if auth == nil {
		return nil
	}
	password, _ := auth.Password()
	return &githttp.BasicAuth{Username: auth.Username(), Password: password}
}

// redact masks credentials servers may echo and bounds err, since errors reach status.
func redact(err error, auth *url.Userinfo) error {
	msg := err.Error()
	if auth != nil {
		password, _ := auth.Password()
		for _, secret := range []string{password, auth.Username()} {
			if secret != "" {
				msg = strings.ReplaceAll(msg, secret, "xxxxx")
			}
		}
	}
	msg = strings.TrimSpace(msg)
	if runes := []rune(msg); len(runes) > maxGitDiagnosticRunes {
		msg = "..." + string(runes[len(runes)-maxGitDiagnosticRunes:])
	}
	return errors.New(msg)
}
