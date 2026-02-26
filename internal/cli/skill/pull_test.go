package skill

import "testing"

func TestParseGitHubURL(t *testing.T) {
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
			name:     "repo root only",
			rawURL:   "https://github.com/peterj/skills",
			wantURL:  "https://github.com/peterj/skills.git",
			wantRef:  "",
			wantPath: "",
		},
		{
			name:     "branch without subpath",
			rawURL:   "https://github.com/peterj/skills/tree/main",
			wantURL:  "https://github.com/peterj/skills.git",
			wantRef:  "main",
			wantPath: "",
		},
		{
			name:     "deeply nested subpath",
			rawURL:   "https://github.com/org/repo/tree/develop/a/b/c/d",
			wantURL:  "https://github.com/org/repo.git",
			wantRef:  "develop",
			wantPath: "a/b/c/d",
		},
		{
			name:    "non-github host",
			rawURL:  "https://gitlab.com/owner/repo",
			wantErr: true,
		},
		{
			name:    "missing repo in path",
			rawURL:  "https://github.com/owner",
			wantErr: true,
		},
		{
			name:    "invalid URL",
			rawURL:  "://not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotURL, gotRef, gotPath, err := parseGitHubURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseGitHubURL(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
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
