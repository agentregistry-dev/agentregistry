// Package gitutil provides shared utilities for cloning Git repositories
// and copying their contents to a target directory.
package gitutil

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
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
		return "", "", "", fmt.Errorf("unsupported host %q, only github.com is supported", u.Host)
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

// CloneAndCopy clones a GitHub repository URL and copies its contents to targetDir.
// It handles parsing the URL, shallow cloning, navigating to subpaths, and cleanup.
func CloneAndCopy(repoURL, targetDir string, verbose bool) error {
	cloneURL, branch, subPath, err := ParseGitHubURL(repoURL)
	if err != nil {
		return fmt.Errorf("parse GitHub URL: %w", err)
	}

	tempDir, err := os.MkdirTemp("", "arctl-git-clone-*")
	if err != nil {
		return fmt.Errorf("create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// git clone --branch works for branches and tags but not commit SHAs.
	// For SHAs, clone the default branch then checkout the specific commit.
	isSHA := isCommitSHA(branch)

	cloneArgs := []string{"clone", "--depth", "1"}
	if branch != "" && !isSHA {
		cloneArgs = append(cloneArgs, "--branch", branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tempDir)

	gitCmd := exec.Command("git", cloneArgs...)
	if verbose {
		gitCmd.Stdout = os.Stdout
		gitCmd.Stderr = os.Stderr
	}
	if err := gitCmd.Run(); err != nil {
		return fmt.Errorf("clone repository: %w", err)
	}

	// For commit SHAs, fetch the specific commit and check it out.
	if isSHA {
		fetchCmd := exec.Command("git", "-C", tempDir, "fetch", "--depth", "1", "origin", branch)
		if verbose {
			fetchCmd.Stdout = os.Stdout
			fetchCmd.Stderr = os.Stderr
		}
		if err := fetchCmd.Run(); err != nil {
			return fmt.Errorf("fetch commit %s: %w", branch, err)
		}

		checkoutCmd := exec.Command("git", "-C", tempDir, "checkout", "FETCH_HEAD")
		if verbose {
			checkoutCmd.Stdout = os.Stdout
			checkoutCmd.Stderr = os.Stderr
		}
		if err := checkoutCmd.Run(); err != nil {
			return fmt.Errorf("checkout commit %s: %w", branch, err)
		}
	}

	return CopyRepoContents(tempDir, subPath, targetDir)
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

// commitSHAPattern matches full (40-char) and abbreviated (7-40 char)
// hexadecimal commit SHAs.
var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,40}$`)

// isCommitSHA returns true if ref looks like a Git commit SHA rather than a
// branch or tag name. This is a heuristic: a 7-40 character hex string.
func isCommitSHA(ref string) bool {
	return commitSHAPattern.MatchString(ref)
}
