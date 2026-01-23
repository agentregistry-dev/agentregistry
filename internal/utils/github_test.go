package utils_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/utils"
)

func TestParseGitHubURL(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantOwner string
		wantRepo  string
		wantErr   bool
	}{
		{"https basic", "https://github.com/owner/repo", "owner", "repo", false},
		{"https with .git", "https://github.com/owner/repo.git", "owner", "repo", false},
		{"https trailing slash", "https://github.com/owner/repo/", "owner", "repo", false},
		{"ssh basic", "git@github.com:owner/repo", "owner", "repo", false},
		{"ssh with .git", "git@github.com:owner/repo.git", "owner", "repo", false},
		{"complex owner", "https://github.com/my-org/my-repo", "my-org", "my-repo", false},
		{"invalid not github", "https://gitlab.com/owner/repo", "", "", true},
		{"invalid no repo", "https://github.com/owner", "", "", true},
		{"invalid empty", "", "", "", true},
		{"invalid malformed", "not-a-url", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := utils.ParseGitHubURL(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGitHubURL(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			if got.Owner != tt.wantOwner {
				t.Errorf("ParseGitHubURL(%q).Owner = %q, want %q", tt.input, got.Owner, tt.wantOwner)
			}
			if got.Repo != tt.wantRepo {
				t.Errorf("ParseGitHubURL(%q).Repo = %q, want %q", tt.input, got.Repo, tt.wantRepo)
			}
			if got.Branch != "main" {
				t.Errorf("ParseGitHubURL(%q).Branch = %q, want %q", tt.input, got.Branch, "main")
			}
		})
	}
}

func TestGitHubRepoInfo_GetGitHubRepoURL(t *testing.T) {
	tests := []struct {
		name   string
		info   *utils.GitHubRepoInfo
		wanted string
	}{
		{"basic", &utils.GitHubRepoInfo{Owner: "owner", Repo: "repo", Branch: "main"}, "https://github.com/owner/repo"},
		{"with org", &utils.GitHubRepoInfo{Owner: "my-org", Repo: "my-repo", Branch: "develop"}, "https://github.com/my-org/my-repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.info.GetGitHubRepoURL()
			if got != tt.wanted {
				t.Errorf("GetGitHubRepoURL() = %q, want %q", got, tt.wanted)
			}
		})
	}
}

func TestFetchGitHubRawFile_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	info := &utils.GitHubRepoInfo{
		Owner:  "modelcontextprotocol",
		Repo:   "servers",
		Branch: "main",
	}

	content, err := utils.FetchGitHubRawFile(info, "README.md")
	if err != nil {
		t.Fatalf("FetchGitHubRawFile() error = %v", err)
	}

	if len(content) == 0 {
		t.Error("FetchGitHubRawFile() returned empty content")
	}
}

func TestFetchGitHubRawFile_NotFound(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	info := &utils.GitHubRepoInfo{
		Owner:  "modelcontextprotocol",
		Repo:   "servers",
		Branch: "main",
	}

	_, err := utils.FetchGitHubRawFile(info, "nonexistent-file-12345.yaml")
	if err == nil {
		t.Error("FetchGitHubRawFile() expected error for nonexistent file")
	}
}
