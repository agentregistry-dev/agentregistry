// Package gitutil pins and fetches git sources for the registry.
package gitutil

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
)

const maxGitDiagnosticRunes = 4096

var (
	// ErrUnsupportedHost is a terminal error for a web URL on a host other than GitHub or GitLab.
	ErrUnsupportedHost = errors.New("unsupported git host")
	// ErrRefNotFound is a terminal error for a ref that names no commit on the remote.
	ErrRefNotFound = errors.New("git ref not found")
)

// ParseGitURL splits a GitHub/GitLab web URL, or any host's .git clone URL, into clone URL, branch, and subpath.
func ParseGitURL(rawURL string) (cloneURL, branch, subPath string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}

	if u.Host == "" {
		return "", "", "", fmt.Errorf("invalid Git URL: expected absolute URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", "", "", fmt.Errorf("invalid Git URL: unsupported scheme %q", u.Scheme)
	}

	// Split the escaped path so %2F in branch names survives.
	rawPath := u.EscapedPath()

	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("invalid Git URL: expected at least namespace/repo in path")
	}

	if gitLabMarker := slices.Index(parts, "-"); gitLabMarker >= 2 {
		return parseGitLabStyleURL(u, parts, gitLabMarker)
	}
	if strings.Contains(strings.ToLower(u.Host), "gitlab") {
		return parseRepoRootURL(u, parts)
	}
	if u.Host == "github.com" {
		return parseGitHubStyleURL(u, parts)
	}
	// Other hosts' web layouts vary, so only their clone URLs are accepted.
	if strings.HasSuffix(parts[len(parts)-1], ".git") {
		return parseRepoRootURL(u, parts)
	}

	return "", "", "", fmt.Errorf("%w: %q (pass the repository clone URL, ending in .git)", ErrUnsupportedHost, u.Host)
}

func parseGitHubStyleURL(u *url.URL, parts []string) (cloneURL, branch, subPath string, err error) {
	namespace := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	cloneURL = fmt.Sprintf("%s://%s/%s/%s.git", u.Scheme, u.Host, namespace, repo)

	if len(parts) >= 4 && (parts[2] == "tree" || parts[2] == "blob") {
		branch, _ = url.PathUnescape(parts[3])
		if len(parts) > 4 {
			raw := strings.Join(parts[4:], "/")
			subPath, _ = url.PathUnescape(raw)
		}
	}

	return cloneURL, branch, subPath, nil
}

// parseRepoRootURL treats the whole path as the repository.
func parseRepoRootURL(u *url.URL, parts []string) (cloneURL, branch, subPath string, err error) {
	repoParts := append([]string(nil), parts...)
	repoParts[len(repoParts)-1] = strings.TrimSuffix(repoParts[len(repoParts)-1], ".git")
	cloneURL = fmt.Sprintf("%s://%s/%s.git", u.Scheme, u.Host, strings.Join(repoParts, "/"))
	return cloneURL, "", "", nil
}

func parseGitLabStyleURL(u *url.URL, parts []string, marker int) (cloneURL, branch, subPath string, err error) {
	repoParts := append([]string(nil), parts[:marker]...)
	repoParts[len(repoParts)-1] = strings.TrimSuffix(repoParts[len(repoParts)-1], ".git")
	cloneURL = fmt.Sprintf("%s://%s/%s.git", u.Scheme, u.Host, strings.Join(repoParts, "/"))

	if len(parts) >= marker+3 && (parts[marker+1] == "tree" || parts[marker+1] == "blob") {
		branch, _ = url.PathUnescape(parts[marker+2])
		if len(parts) > marker+3 {
			raw := strings.Join(parts[marker+3:], "/")
			subPath, _ = url.PathUnescape(raw)
		}
	}

	return cloneURL, branch, subPath, nil
}
