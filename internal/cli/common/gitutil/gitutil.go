// Package gitutil provides shared utilities for cloning Git repositories
// and copying their contents to a target directory.
package gitutil

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	// ErrUnsupportedHost is returned for a non-github.com host. It is a
	// permanent (terminal) condition — callers can errors.Is it to avoid
	// retrying a host that will never be supported.
	ErrUnsupportedHost = errors.New("unsupported git host")
	// ErrRefNotFound is returned when a ref resolves to no commit on the remote
	// (deleted branch/tag, typo, or a short/non-existent SHA). Terminal:
	// retrying the same ref will not find it.
	ErrRefNotFound = errors.New("git ref not found")
)

// ParseGitHubURL parses a GitHub URL into its clone URL, branch, and subdirectory path.
// Supported formats:
//   - https://github.com/owner/repo/tree/branch/path/to/dir
//   - https://github.com/owner/repo
//
// Branch names containing slashes (e.g. feature/my-branch) are supported when
// encoded as %2F in the URL. The raw (escaped) path is used for splitting so
// the encoded branch segment is preserved, then unescaped for the return value.
func ParseGitHubURL(rawURL string) (cloneURL, branch, subPath string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}

	if u.Host != "github.com" {
		return "", "", "", fmt.Errorf("%w: %q, only github.com is supported", ErrUnsupportedHost, u.Host)
	}

	// Use EscapedPath so that percent-encoded segments (e.g. %2F in branch
	// names) are not decoded before splitting on "/".
	rawPath := u.EscapedPath()

	// Path is like /owner/repo or /owner/repo/tree/branch/sub/path
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("invalid GitHub URL: expected at least owner/repo in path")
	}

	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	// If URL contains /tree/<branch>/..., extract branch and subpath.
	// The branch segment is unescaped so encoded slashes (%2F) become real
	// slashes in the returned branch name.
	if len(parts) >= 4 && parts[2] == "tree" {
		branch, _ = url.PathUnescape(parts[3])
		if len(parts) > 4 {
			raw := strings.Join(parts[4:], "/")
			subPath, _ = url.PathUnescape(raw)
		}
	}

	return cloneURL, branch, subPath, nil
}

// CloneAndCopyContext clones a GitHub repository URL and copies its contents to
// targetDir. It handles parsing the URL, shallow cloning, navigating to
// subpaths, and cleanup.
//
// branch, commit, and subPath are explicit overrides. When branch and subPath
// are empty, the values parsed from the URL (e.g.
// https://github.com/o/r/tree/<branch>/<sub>) are used. The URL-derived ref is
// always treated as a branch; callers wanting to pin a commit SHA must set the
// commit argument explicitly. branch is passed to `git clone --branch`; commit
// triggers a fetch + checkout after the clone.
//
// Every git invocation runs under ctx, so a caller can bound
// clone/fetch/checkout time (and disk/CPU runaway) by passing a
// context.WithTimeout. ctx cancellation kills the git child process.
func CloneAndCopyContext(ctx context.Context, repoURL, branch, commit, subPath, targetDir string, verbose bool) error {
	cloneURL, urlBranch, urlSubPath, err := ParseGitHubURL(repoURL)
	if err != nil {
		return fmt.Errorf("parse GitHub URL: %w", err)
	}
	if branch == "" {
		branch = urlBranch
	}
	if subPath == "" {
		subPath = urlSubPath
	}
	// Guard against argument injection: branch/commit are passed positionally to
	// git, but a value starting with "-" would be parsed as an option.
	if err := safeGitRef(branch); err != nil {
		return err
	}
	if err := safeGitRef(commit); err != nil {
		return err
	}

	tempDir, err := os.MkdirTemp("", "arctl-git-clone-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	cloneArgs := []string{"clone", "--depth", "1"}
	if branch != "" {
		cloneArgs = append(cloneArgs, "--branch", branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tempDir)

	gitCmd := exec.CommandContext(ctx, "git", cloneArgs...)
	if verbose {
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
	}
	if err := gitCmd.Run(); err != nil {
		return fmt.Errorf("clone repository: %w", err)
	}

	if commit != "" {
		fetchCmd := exec.CommandContext(ctx, "git", "-C", tempDir, "fetch", "--depth", "1", "origin", commit)
		if verbose {
			fetchCmd.Stdout = os.Stdout
			fetchCmd.Stderr = os.Stderr
		}
		if err := fetchCmd.Run(); err != nil {
			return fmt.Errorf("fetch commit %s: %w", commit, err)
		}

		checkoutCmd := exec.CommandContext(ctx, "git", "-C", tempDir, "checkout", "FETCH_HEAD")
		if verbose {
			checkoutCmd.Stdout = os.Stdout
			checkoutCmd.Stderr = os.Stderr
		}
		if err := checkoutCmd.Run(); err != nil {
			return fmt.Errorf("checkout commit %s: %w", commit, err)
		}
	}

	return CopyRepoContents(tempDir, subPath, targetDir)
}

// safeGitRef rejects a ref/branch/commit that git could mis-parse as a
// command-line option (argument injection): the value must not begin with "-".
// An empty value is allowed (callers treat it as "unset"). Values are passed
// positionally to git, never through a shell, so no further quoting is needed.
func safeGitRef(ref string) error {
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("invalid git ref %q: must not start with '-'", ref)
	}
	return nil
}

// isFullCommitSHA reports whether s is a full 40-character hex commit SHA.
func isFullCommitSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') && (r < 'A' || r > 'F') {
			return false
		}
	}
	return true
}

// ResolveRefContext resolves a branch, tag, or HEAD to a concrete commit SHA on
// the remote WITHOUT cloning, using `git ls-remote`. A ref that is already a
// full 40-char commit SHA is returned unchanged (lowercased). An empty ref
// (after the URL-embedded branch is considered) resolves the remote's default
// branch (HEAD). Only github.com URLs are supported (see ParseGitHubURL). ctx
// bounds the ls-remote call. A ref that resolves to no commit returns
// ErrRefNotFound (terminal).
func ResolveRefContext(ctx context.Context, repoURL, ref string) (string, error) {
	if isFullCommitSHA(ref) {
		return strings.ToLower(ref), nil
	}
	cloneURL, urlBranch, _, err := ParseGitHubURL(repoURL)
	if err != nil {
		return "", fmt.Errorf("parse GitHub URL: %w", err)
	}
	if ref == "" {
		ref = urlBranch
	}
	lsRef := ref
	if lsRef == "" {
		lsRef = "HEAD"
	}
	if err := safeGitRef(lsRef); err != nil {
		return "", err
	}
	out, err := exec.CommandContext(ctx, "git", "ls-remote", cloneURL, lsRef).Output()
	if err != nil {
		return "", fmt.Errorf("git ls-remote %s %q: %w", cloneURL, lsRef, err)
	}
	sha := firstLSRemoteSHA(string(out), lsRef)
	if sha == "" {
		return "", fmt.Errorf("%w: %q in %s", ErrRefNotFound, lsRef, cloneURL)
	}
	return sha, nil
}

// firstLSRemoteSHA selects the commit SHA from `git ls-remote` output (lines of
// "<sha>\t<refname>") for the queried ref. Preference order makes an ambiguous
// query (e.g. a name that is both a branch and a tag) deterministic, following
// git's own ref precedence (tags before heads), and resolves annotated tags to
// the commit they point at:
//  1. the dereferenced commit of an exact refs/tags/<ref> ("…^{}"),
//  2. an exact refs/tags/<ref>,
//  3. an exact refs/heads/<ref>,
//  4. any dereferenced commit ("…^{}"),
//  5. the first SHA.
func firstLSRemoteSHA(out, ref string) string {
	wantHead := "refs/heads/" + ref
	wantTag := "refs/tags/" + ref
	var first, anyDeref, tag, tagDeref, head string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		sha := fields[0]
		if first == "" {
			first = sha
		}
		if len(fields) < 2 {
			continue
		}
		name := fields[1]
		switch {
		case name == wantTag+"^{}":
			tagDeref = sha
			anyDeref = sha
		case strings.HasSuffix(name, "^{}"):
			anyDeref = sha
		case name == wantTag:
			tag = sha
		case name == wantHead:
			head = sha
		}
	}
	switch {
	case tagDeref != "":
		return tagDeref
	case tag != "":
		return tag
	case head != "":
		return head
	case anyDeref != "":
		return anyDeref
	default:
		return first
	}
}

// resolveSubPath validates and resolves a subPath within repoDir, returning
// the resolved source directory. It rejects absolute paths and paths that
// escape the repository root via directory traversal.
func resolveSubPath(repoDir, subPath string) (string, error) {
	if filepath.IsAbs(subPath) {
		return "", fmt.Errorf("subpath %q must be relative", subPath)
	}

	srcDir := filepath.Join(repoDir, filepath.Clean(subPath))

	absRepo, err := filepath.Abs(repoDir)
	if err != nil {
		return "", fmt.Errorf("resolve repo directory: %w", err)
	}
	absSrc, err := filepath.Abs(srcDir)
	if err != nil {
		return "", fmt.Errorf("resolve subpath directory: %w", err)
	}
	if !strings.HasPrefix(absSrc, absRepo+string(filepath.Separator)) && absSrc != absRepo {
		return "", fmt.Errorf("subpath %q escapes repository directory", subPath)
	}

	if _, err := os.Stat(srcDir); os.IsNotExist(err) {
		return "", fmt.Errorf("subdirectory %q not found in repository", subPath)
	}

	return srcDir, nil
}

// CopyRepoContents copies files from a cloned repository to the output directory.
// It navigates to the subPath if specified and skips the .git directory.
// Symlinks are skipped to prevent symlink traversal attacks from untrusted repos.
func CopyRepoContents(repoDir, subPath, targetDir string) error {
	srcDir := repoDir
	if subPath != "" {
		resolved, err := resolveSubPath(repoDir, subPath)
		if err != nil {
			return err
		}
		srcDir = resolved
	}

	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("create target directory: %w", err)
	}

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("read source directory: %w", err)
	}

	for _, entry := range entries {
		if entry.Name() == ".git" {
			continue
		}

		// Skip symlinks to prevent traversal attacks from untrusted repos
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(targetDir, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy directory %s: %w", entry.Name(), err)
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("copy file %s: %w", entry.Name(), err)
			}
		}
	}

	return nil
}

// CopyDir recursively copies a directory tree, skipping symlinks.
func CopyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		// Skip symlinks to prevent traversal attacks
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := CopyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// CopyFile copies a single regular file. The caller must ensure src is not a symlink.
func CopyFile(src, dst string) error {
	// Verify the source is a regular file via Lstat (does not follow symlinks)
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to copy symlink: %s", src)
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	return os.Chmod(dst, srcInfo.Mode().Perm())
}

// RepoNameFromCloneURL extracts the repository name from a clone URL
// (e.g., "https://github.com/org/my-repo.git" -> "my-repo").
func RepoNameFromCloneURL(cloneURL string) string {
	idx := strings.LastIndex(cloneURL, "/")
	if idx < 0 {
		return ""
	}
	return strings.TrimSuffix(cloneURL[idx+1:], ".git")
}
