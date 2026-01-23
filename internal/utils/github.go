package utils

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type GitHubRepoInfo struct {
	Owner  string
	Repo   string
	Branch string
}

func ParseGitHubURL(rawURL string) (*GitHubRepoInfo, error) {
	if strings.HasPrefix(rawURL, "git@github.com:") {
		path := strings.TrimPrefix(rawURL, "git@github.com:")
		path = strings.TrimSuffix(path, ".git")
		parts := strings.Split(path, "/")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid GitHub SSH URL: %s", rawURL)
		}
		return &GitHubRepoInfo{
			Owner:  parts[0],
			Repo:   parts[1],
			Branch: "main",
		}, nil
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	if parsed.Host != "github.com" {
		return nil, fmt.Errorf("not a GitHub URL: %s", rawURL)
	}

	path := strings.TrimPrefix(parsed.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.Split(path, "/")

	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid GitHub URL path: %s", rawURL)
	}

	return &GitHubRepoInfo{
		Owner:  parts[0],
		Repo:   parts[1],
		Branch: "main",
	}, nil
}

func FetchGitHubRawFile(info *GitHubRepoInfo, filePath string) ([]byte, error) {
	rawURL := fmt.Sprintf(
		"https://raw.githubusercontent.com/%s/%s/%s/%s",
		info.Owner, info.Repo, info.Branch, filePath,
	)

	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch file from GitHub: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("file not found in repository: %s (branch: %s)", filePath, info.Branch)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d fetching %s from GitHub", resp.StatusCode, filePath)
	}

	return io.ReadAll(resp.Body)
}

func (info *GitHubRepoInfo) GetGitHubRepoURL() string {
	return fmt.Sprintf("https://github.com/%s/%s", info.Owner, info.Repo)
}
