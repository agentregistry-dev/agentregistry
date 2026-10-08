package gitutil

import (
	"errors"
	"testing"
)

func TestParseGitURL(t *testing.T) {
	tests := []struct {
		name     string
		rawURL   string
		wantURL  string
		wantRef  string
		wantPath string
		wantErr  bool
	}{
		{
			name:     "full URL with branch and subpath",
			rawURL:   "https://github.com/peterj/skills/tree/main/skills/argocd-cli-setup",
			wantURL:  "https://github.com/peterj/skills.git",
			wantRef:  "main",
			wantPath: "skills/argocd-cli-setup",
		},
		{
			name:    "repo root only",
			rawURL:  "https://github.com/peterj/skills",
			wantURL: "https://github.com/peterj/skills.git",
		},
		{
			name:    "branch without subpath",
			rawURL:  "https://github.com/peterj/skills/tree/main",
			wantURL: "https://github.com/peterj/skills.git",
			wantRef: "main",
		},
		{
			name:     "deeply nested subpath",
			rawURL:   "https://github.com/org/repo/tree/develop/a/b/c/d",
			wantURL:  "https://github.com/org/repo.git",
			wantRef:  "develop",
			wantPath: "a/b/c/d",
		},
		{
			name:    "trailing slash on repo URL",
			rawURL:  "https://github.com/owner/repo/",
			wantURL: "https://github.com/owner/repo.git",
		},
		{
			name:     "blob URL with file path",
			rawURL:   "https://github.com/owner/repo/blob/main/README.md",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "main",
			wantPath: "README.md",
		},
		{
			name:    "three path segments without tree",
			rawURL:  "https://github.com/owner/repo/issues",
			wantURL: "https://github.com/owner/repo.git",
		},
		{
			name:    "repo name with dots and hyphens",
			rawURL:  "https://github.com/my-org/my-repo.v2",
			wantURL: "https://github.com/my-org/my-repo.v2.git",
		},
		{
			name:     "URL with query params stripped",
			rawURL:   "https://github.com/owner/repo/tree/main/dir?tab=readme",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "main",
			wantPath: "dir",
		},
		{
			name:     "URL with fragment stripped",
			rawURL:   "https://github.com/owner/repo/tree/main/dir#section",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "main",
			wantPath: "dir",
		},
		{
			name:     "tag-style ref with dots",
			rawURL:   "https://github.com/owner/repo/tree/v1.2.3/src",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "v1.2.3",
			wantPath: "src",
		},
		{
			name:     "encoded slash in branch preserved",
			rawURL:   "https://github.com/owner/repo/tree/feature%2Fmy-branch/path",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "feature/my-branch",
			wantPath: "path",
		},
		{
			name:    "repo URL ending with .git",
			rawURL:  "https://github.com/owner/repo.git",
			wantURL: "https://github.com/owner/repo.git",
		},
		{
			name:     "repo URL with .git and tree path",
			rawURL:   "https://github.com/owner/repo.git/tree/main/src",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "main",
			wantPath: "src",
		},
		{
			name:    "gitlab repo root",
			rawURL:  "https://gitlab.com/owner/repo",
			wantURL: "https://gitlab.com/owner/repo.git",
		},
		{
			name:    "gitlab nested group repo root",
			rawURL:  "https://gitlab.com/org/platform/team/skills",
			wantURL: "https://gitlab.com/org/platform/team/skills.git",
		},
		{
			name:     "self-hosted gitlab tree URL",
			rawURL:   "https://gitlabe2.ext.net.nokia.com/chalapat/fpm-tools/-/tree/main/ai-skills/artifactory-auth-migration",
			wantURL:  "https://gitlabe2.ext.net.nokia.com/chalapat/fpm-tools.git",
			wantRef:  "main",
			wantPath: "ai-skills/artifactory-auth-migration",
		},
		{
			name:     "self-hosted gitlab blob URL",
			rawURL:   "https://gitlabe2.ext.net.nokia.com/chalapat/fpm-tools/-/blob/main/ai-skills/artifactory-auth-migration/.cursor/skills/artifactory-auth-migration/SKILL.md?ref_type=heads",
			wantURL:  "https://gitlabe2.ext.net.nokia.com/chalapat/fpm-tools.git",
			wantRef:  "main",
			wantPath: "ai-skills/artifactory-auth-migration/.cursor/skills/artifactory-auth-migration/SKILL.md",
		},
		{
			name:     "self-hosted gitlab nested group tree URL",
			rawURL:   "https://gitlab.example.com/org/platform/team/skills/-/tree/feature%2Fgitlab-support/cursor/skill",
			wantURL:  "https://gitlab.example.com/org/platform/team/skills.git",
			wantRef:  "feature/gitlab-support",
			wantPath: "cursor/skill",
		},
		{
			name:    "bitbucket server clone URL",
			rawURL:  "https://bitbucket.example.com/scm/proj/repo.git",
			wantURL: "https://bitbucket.example.com/scm/proj/repo.git",
		},
		{
			name:    "bitbucket server clone URL with trailing slash",
			rawURL:  "https://bitbucket.example.com/scm/proj/repo.git/",
			wantURL: "https://bitbucket.example.com/scm/proj/repo.git",
		},
		{
			name:    "bitbucket cloud clone URL",
			rawURL:  "https://bitbucket.org/workspace/repo.git",
			wantURL: "https://bitbucket.org/workspace/repo.git",
		},
		{
			name:    "gitea clone URL",
			rawURL:  "https://git.example.com/org/repo.git",
			wantURL: "https://git.example.com/org/repo.git",
		},
		{
			name:    "github enterprise clone URL on company domain",
			rawURL:  "https://github.corp.example.com/org/repo.git",
			wantURL: "https://github.corp.example.com/org/repo.git",
		},
		{
			name:    "bitbucket server web URL is not a clone URL",
			rawURL:  "https://bitbucket.example.com/projects/PROJ/repos/repo/browse",
			wantErr: true,
		},
		{
			name:    "unknown host repo root without .git",
			rawURL:  "https://git.example.com/org/repo",
			wantErr: true,
		},
		{
			name:    "missing repo in path",
			rawURL:  "https://github.com/owner",
			wantErr: true,
		},
		{
			name:    "empty path",
			rawURL:  "https://github.com",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			rawURL:  "://not-a-url",
			wantErr: true,
		},
		{
			name:    "unsupported scheme",
			rawURL:  "ssh://github.com/owner/repo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotRef, gotPath, err := ParseGitURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseGitURL(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if gotURL != tt.wantURL {
				t.Errorf("cloneURL = %q, want %q", gotURL, tt.wantURL)
			}
			if gotRef != tt.wantRef {
				t.Errorf("branch = %q, want %q", gotRef, tt.wantRef)
			}
			if gotPath != tt.wantPath {
				t.Errorf("subPath = %q, want %q", gotPath, tt.wantPath)
			}
		})
	}
}

// TestParseGitURLUnsupportedHost pins the sentinel, since callers classify on
// it: the Skill controller and the plugin source treat ErrUnsupportedHost as
// terminal and stop retrying.
func TestParseGitURLUnsupportedHost(t *testing.T) {
	gotURL, gotRef, gotPath, err := ParseGitURL("https://bitbucket.example.com/projects/PROJ/repos/repo/browse")
	if !errors.Is(err, ErrUnsupportedHost) {
		t.Fatalf("error = %v, want ErrUnsupportedHost", err)
	}
	if gotURL != "" || gotRef != "" || gotPath != "" {
		t.Errorf("got (%q, %q, %q), want zero values on error", gotURL, gotRef, gotPath)
	}
}
