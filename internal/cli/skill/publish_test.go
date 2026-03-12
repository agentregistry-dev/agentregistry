package skill

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestParseSkillFrontmatter(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantName    string
		wantDesc    string
		wantErr     bool
		errContains string
	}{
		{
			name: "valid frontmatter",
			content: `---
name: my-skill
description: A test skill
---
# My Skill
Some content here.
`,
			wantName: "my-skill",
			wantDesc: "A test skill",
		},
		{
			name: "name only (missing description)",
			content: `---
name: simple-skill
---
Body text.
`,
			wantErr:     true,
			errContains: "missing required field: description",
		},
		{
			name: "description only (missing name)",
			content: `---
description: no name provided
---
Body.
`,
			wantErr:     true,
			errContains: "missing required field: name",
		},
		{
			name:        "empty file",
			content:     "",
			wantErr:     true,
			errContains: "SKILL.md is empty",
		},
		{
			name:        "no frontmatter delimiters",
			content:     "just some text\nno yaml here\n",
			wantErr:     true,
			errContains: "missing YAML frontmatter",
		},
		{
			name: "only opening delimiter",
			content: `---
name: orphan
`,
			wantErr:     true,
			errContains: "missing YAML frontmatter",
		},
		{
			name: "invalid yaml",
			content: `---
name: [invalid
---
`,
			wantErr:     true,
			errContains: "failed to parse SKILL.md frontmatter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			skillMd := filepath.Join(dir, "SKILL.md")
			if err := os.WriteFile(skillMd, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write SKILL.md: %v", err)
			}

			fm, err := parseSkillFrontmatter(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseSkillFrontmatter() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				if tt.errContains != "" && err != nil {
					if got := err.Error(); !contains(got, tt.errContains) {
						t.Errorf("error = %q, want it to contain %q", got, tt.errContains)
					}
				}
				return
			}
			if fm.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", fm.Name, tt.wantName)
			}
			if fm.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", fm.Description, tt.wantDesc)
			}
		})
	}
}

func TestParseSkillFrontmatter_MissingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := parseSkillFrontmatter(dir)
	if err == nil {
		t.Fatal("expected error for missing SKILL.md, got nil")
	}
	if !contains(err.Error(), "failed to open SKILL.md") {
		t.Errorf("error = %q, want it to contain 'failed to open SKILL.md'", err.Error())
	}
}

func TestBuildSkillFromGitHub(t *testing.T) {
	// Save and restore package-level vars
	origGithub := gitRepository
	origVersion := versionFlag
	origGithubRawBase := githubRawBaseURL
	t.Cleanup(func() {
		gitRepository = origGithub
		versionFlag = origVersion
		githubRawBaseURL = origGithubRawBase
	})
	mockGitHubSkillMdCheck(t)

	tests := []struct {
		name        string
		skillMd     string
		github      string
		version     string
		wantName    string
		wantVer     string
		wantRepoURL string
	}{
		{
			name: "basic github publish",
			skillMd: `---
name: my-skill
description: A skill
---
`,
			github:      "https://github.com/org/repo",
			version:     "1.0.0",
			wantName:    "my-skill",
			wantVer:     "1.0.0",
			wantRepoURL: "https://github.com/org/repo",
		},
		{
			name: "full tree URL with branch and path",
			skillMd: `---
name: nested-skill
description: Nested
---
`,
			github:      "https://github.com/org/repo/tree/main/skills/my-skill",
			version:     "1.0.0",
			wantName:    "nested-skill",
			wantVer:     "1.0.0",
			wantRepoURL: "https://github.com/org/repo/tree/main/skills/my-skill",
		},
		{
			name: "tree URL with branch only",
			skillMd: `---
name: branch-skill
description: Branch
---
`,
			github:      "https://github.com/org/repo/tree/develop",
			version:     "1.0.0",
			wantName:    "branch-skill",
			wantVer:     "1.0.0",
			wantRepoURL: "https://github.com/org/repo/tree/develop",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(tt.skillMd), 0644); err != nil {
				t.Fatalf("failed to write SKILL.md: %v", err)
			}

			gitRepository = tt.github
			versionFlag = tt.version

			skill, err := buildSkillFromGitHub(dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if skill.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", skill.Name, tt.wantName)
			}
			if skill.Version != tt.wantVer {
				t.Errorf("Version = %q, want %q", skill.Version, tt.wantVer)
			}
			if skill.Repository == nil {
				t.Fatal("Repository is nil, expected it to be set")
			}
			if skill.Repository.URL != tt.wantRepoURL {
				t.Errorf("Repository.URL = %q, want %q", skill.Repository.URL, tt.wantRepoURL)
			}
			if skill.Repository.Source != "git" {
				t.Errorf("Repository.Source = %q, want %q", skill.Repository.Source, "git")
			}
			if len(skill.Packages) != 0 {
				t.Errorf("Packages should be empty for GitHub publish, got %d", len(skill.Packages))
			}
		})
	}
}

func TestBuildSkillFromGitHub_MissingVersion(t *testing.T) {
	origGithub := gitRepository
	origVersion := versionFlag
	t.Cleanup(func() {
		gitRepository = origGithub
		versionFlag = origVersion
	})

	gitRepository = "https://github.com/org/repo"
	versionFlag = ""

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test\ndescription: test skill\n---\n")

	_, err := buildSkillFromGitHub(dir)
	if err == nil {
		t.Fatal("expected error when --version is missing for GitHub publish, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

func TestBuildSkillFromGitHub_InvalidFrontmatter(t *testing.T) {
	origGithub := gitRepository
	origVersion := versionFlag
	t.Cleanup(func() {
		gitRepository = origGithub
		versionFlag = origVersion
	})

	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("no frontmatter"), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	_, err := buildSkillFromGitHub(dir)
	if err == nil {
		t.Fatal("expected error for invalid frontmatter, got nil")
	}
}

func TestBuildSkillFromGitHub_InvalidURL(t *testing.T) {
	origGithub := gitRepository
	origVersion := versionFlag
	t.Cleanup(func() {
		gitRepository = origGithub
		versionFlag = origVersion
	})

	versionFlag = "1.0.0"

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test\ndescription: test skill\n---\n")

	tests := []struct {
		name        string
		github      string
		errContains string
	}{
		{
			name:        "non-github host",
			github:      "https://gitlab.com/org/repo",
			errContains: "unsupported host",
		},
		{
			name:        "missing repo in path",
			github:      "https://github.com/owner",
			errContains: "expected at least owner/repo",
		},
		{
			name:        "invalid URL",
			github:      "://not-a-url",
			errContains: "invalid URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepository = tt.github

			_, err := buildSkillFromGitHub(dir)
			if err == nil {
				t.Fatal("expected error for invalid GitHub URL, got nil")
			}
			if got := err.Error(); !contains(got, tt.errContains) {
				t.Errorf("error = %q, want it to contain %q", got, tt.errContains)
			}
		})
	}
}

// savePublishFlags saves all publish-related package-level vars and returns a cleanup function.
func savePublishFlags(t *testing.T) {
	t.Helper()
	origVersionFlag := versionFlag
	origDryRunFlag := dryRunFlag
	origGithubRepo := gitRepository
	origDockerImage := dockerImageFlag
	origClient := apiClient
	origGithubRawBaseURL := githubRawBaseURL

	t.Cleanup(func() {
		versionFlag = origVersionFlag
		dryRunFlag = origDryRunFlag
		gitRepository = origGithubRepo
		dockerImageFlag = origDockerImage
		apiClient = origClient
		githubRawBaseURL = origGithubRawBaseURL
	})
}

// mockGitHubSkillMdCheck sets up an httptest server that always returns 200
// for SKILL.md checks, simulating a repository where SKILL.md exists.
func mockGitHubSkillMdCheck(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	githubRawBaseURL = srv.URL
}

func TestRunPublish_NilClient(t *testing.T) {
	savePublishFlags(t)
	apiClient = nil
	gitRepository = "https://github.com/org/repo"

	err := runPublish(nil, []string{"."})
	if err == nil {
		t.Fatal("expected error for nil apiClient, got nil")
	}
	if !contains(err.Error(), "API client not initialized") {
		t.Errorf("error = %q, want it to contain 'API client not initialized'", err.Error())
	}
}

func TestRunPublish_NonExistentPathUsesDirectMode(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var skill models.SkillJSON
		json.NewDecoder(r.Body).Decode(&skill)
		// Non-existent path is treated as a skill name in direct mode
		if skill.Name != "/nonexistent/path/to/skill" {
			t.Errorf("skill name = %q, want %q", skill.Name, "/nonexistent/path/to/skill")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"

	err := runPublish(nil, []string{"/nonexistent/path/to/skill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_DirWithoutSkillMdReturnsError(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dir string)
	}{
		{
			name:  "empty directory",
			setup: func(dir string) {},
		},
		{
			name: "directory with other files but no SKILL.md",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "README.md"), "no skill here")
			},
		},
		{
			name: "directory with subdirectories but none containing SKILL.md",
			setup: func(dir string) {
				sub := filepath.Join(dir, "sub")
				os.MkdirAll(sub, 0755)
				writeFile(t, filepath.Join(sub, "README.md"), "not a skill")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			savePublishFlags(t)
			apiClient = client.NewClient("http://localhost:0", "")
			gitRepository = "https://github.com/org/repo"
			versionFlag = "1.0.0"

			dir := t.TempDir()
			tt.setup(dir)

			err := runPublish(nil, []string{dir})
			if err == nil {
				t.Fatal("expected error for directory without SKILL.md, got nil")
			}
			if !contains(err.Error(), "no valid skills found at path") {
				t.Errorf("error = %q, want it to contain 'no valid skills found at path'", err.Error())
			}
		})
	}
}

func TestRunPublish_GitHubDryRun(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)
	apiClient = client.NewClient("http://localhost:0", "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	dryRunFlag = true

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: dry-test\ndescription: dry\n---\n")

	err := runPublish(nil, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_GitHubSuccess(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v0/skills" {
			var skill models.SkillJSON
			if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
				t.Errorf("failed to decode request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if skill.Name != "my-skill" {
				t.Errorf("skill name = %q, want %q", skill.Name, "my-skill")
			}
			if skill.Version != "1.0.0" {
				t.Errorf("skill version = %q, want %q", skill.Version, "1.0.0")
			}
			if skill.Repository == nil || skill.Repository.URL != "https://github.com/org/repo" {
				t.Errorf("skill repository URL not set correctly")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	dryRunFlag = false

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: my-skill\ndescription: test\n---\n")

	err := runPublish(nil, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_GitHubAPIError(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	dryRunFlag = false

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: fail-skill\ndescription: fails\n---\n")

	err := runPublish(nil, []string{dir})
	if err == nil {
		t.Fatal("expected error for API failure, got nil")
	}
	if !contains(err.Error(), "failed to publish skill") {
		t.Errorf("error = %q, want it to contain 'failed to publish skill'", err.Error())
	}
}

func TestRunPublish_ParentDirWithSubSkillsReturnsError(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	dryRunFlag = false

	// Parent dir has subdirectories with SKILL.md, but no SKILL.md itself.
	// isValidSkillDir only checks the given directory, not subdirectories.
	dir := t.TempDir()
	for _, name := range []string{"skill-a", "skill-b"} {
		sub := filepath.Join(dir, name)
		os.MkdirAll(sub, 0755)
		writeFile(t, filepath.Join(sub, "SKILL.md"), "---\nname: "+name+"\ndescription: test\n---\n")
	}

	err := runPublish(nil, []string{dir})
	if err == nil {
		t.Fatal("expected error for parent dir without SKILL.md, got nil")
	}
	if !contains(err.Error(), "no valid skills found at path") {
		t.Errorf("error = %q, want it to contain 'no valid skills found at path'", err.Error())
	}
}

func TestRunPublish_GitHubMissingVersion(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = ""
	dryRunFlag = false

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test\ndescription: test skill\n---\n")

	err := runPublish(nil, []string{dir})
	if err == nil {
		t.Fatal("expected error when --version is missing for GitHub, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

// --- Direct registration mode tests ---

func TestBuildSkillDirect(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	tests := []struct {
		name        string
		skillName   string
		github      string
		version     string
		desc        string
		wantName    string
		wantVer     string
		wantDesc    string
		wantRepoURL string
	}{
		{
			name:        "basic direct publish",
			skillName:   "my-remote-skill",
			github:      "https://github.com/org/repo",
			version:     "1.0.0",
			desc:        "A remote skill",
			wantName:    "my-remote-skill",
			wantVer:     "1.0.0",
			wantDesc:    "A remote skill",
			wantRepoURL: "https://github.com/org/repo",
		},
		{
			name:        "name is lowercased",
			skillName:   "My-Skill",
			github:      "https://github.com/org/repo",
			version:     "2.0.0",
			wantName:    "my-skill",
			wantVer:     "2.0.0",
			wantRepoURL: "https://github.com/org/repo",
		},
		{
			name:        "empty description is allowed",
			skillName:   "no-desc",
			github:      "https://github.com/org/repo",
			version:     "1.0.0",
			wantName:    "no-desc",
			wantVer:     "1.0.0",
			wantDesc:    "",
			wantRepoURL: "https://github.com/org/repo",
		},
		{
			name:        "tree URL with branch and path",
			skillName:   "nested-skill",
			github:      "https://github.com/org/repo/tree/main/skills/nested",
			version:     "1.0.0",
			wantName:    "nested-skill",
			wantVer:     "1.0.0",
			wantRepoURL: "https://github.com/org/repo/tree/main/skills/nested",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gitRepository = tt.github
			versionFlag = tt.version
			publishDesc = tt.desc

			skill, err := buildSkillDirectGitHub(tt.skillName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if skill.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", skill.Name, tt.wantName)
			}
			if skill.Version != tt.wantVer {
				t.Errorf("Version = %q, want %q", skill.Version, tt.wantVer)
			}
			if skill.Description != tt.wantDesc {
				t.Errorf("Description = %q, want %q", skill.Description, tt.wantDesc)
			}
			if skill.Repository == nil {
				t.Fatal("Repository is nil")
			}
			if skill.Repository.URL != tt.wantRepoURL {
				t.Errorf("Repository.URL = %q, want %q", skill.Repository.URL, tt.wantRepoURL)
			}
			if skill.Repository.Source != "git" {
				t.Errorf("Repository.Source = %q, want %q", skill.Repository.Source, "git")
			}
			if len(skill.Packages) != 0 {
				t.Errorf("Packages should be empty, got %d", len(skill.Packages))
			}
		})
	}
}

func TestBuildSkillDirect_MissingGithub(t *testing.T) {
	savePublishFlags(t)
	gitRepository = ""
	versionFlag = "1.0.0"

	_, err := buildSkillDirectGitHub("my-skill")
	if err == nil {
		t.Fatal("expected error when --git is missing, got nil")
	}
	if !contains(err.Error(), "--git is required") {
		t.Errorf("error = %q, want it to contain '--git is required'", err.Error())
	}
}

func TestBuildSkillDirect_MissingVersion(t *testing.T) {
	savePublishFlags(t)
	gitRepository = "https://github.com/org/repo"
	versionFlag = ""

	_, err := buildSkillDirectGitHub("my-skill")
	if err == nil {
		t.Fatal("expected error when --version is missing, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

func TestBuildSkillDirect_InvalidURL(t *testing.T) {
	savePublishFlags(t)
	gitRepository = "https://gitlab.com/org/repo"
	versionFlag = "1.0.0"

	_, err := buildSkillDirectGitHub("my-skill")
	if err == nil {
		t.Fatal("expected error for invalid GitHub URL, got nil")
	}
	if !contains(err.Error(), "unsupported host") {
		t.Errorf("error = %q, want it to contain 'unsupported host'", err.Error())
	}
}

func TestRunPublish_DirectGitHub(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v0/skills" {
			var skill models.SkillJSON
			json.NewDecoder(r.Body).Decode(&skill)
			if skill.Name != "remote-skill" {
				t.Errorf("skill name = %q, want %q", skill.Name, "remote-skill")
			}
			if skill.Version != "1.0.0" {
				t.Errorf("skill version = %q, want %q", skill.Version, "1.0.0")
			}
			if skill.Description != "A remote skill" {
				t.Errorf("skill description = %q, want %q", skill.Description, "A remote skill")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	publishDesc = "A remote skill"
	dryRunFlag = false

	// Use a non-existent path name so it's treated as a skill name
	err := runPublish(nil, []string{"remote-skill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_DirectDryRun(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)
	apiClient = client.NewClient("http://localhost:0", "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	publishDesc = "test"
	dryRunFlag = true

	err := runPublish(nil, []string{"dry-run-direct"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_DirectMissingBothFlags(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	gitRepository = ""
	dockerImageFlag = ""
	versionFlag = "1.0.0"

	err := runPublish(nil, []string{"my-skill"})
	if err == nil {
		t.Fatal("expected error when neither flag is set, got nil")
	}
	if !contains(err.Error(), "--git or --docker-image is required") {
		t.Errorf("error = %q, want it to contain '--git or --docker-image is required'", err.Error())
	}
}

func TestRunPublish_DirectMissingVersion(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = ""

	err := runPublish(nil, []string{"my-skill"})
	if err == nil {
		t.Fatal("expected error when --version is missing in direct mode, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

// --- Direct Docker mode tests ---

func TestBuildSkillDirectDocker(t *testing.T) {
	savePublishFlags(t)

	tests := []struct {
		name        string
		skillName   string
		dockerImage string
		version     string
		desc        string
		wantName    string
		wantVer     string
		wantImage   string
	}{
		{
			name:        "basic docker publish",
			skillName:   "my-docker-skill",
			dockerImage: "docker.io/myorg/my-skill:v1.0.0",
			version:     "1.0.0",
			desc:        "A Docker skill",
			wantName:    "my-docker-skill",
			wantVer:     "1.0.0",
			wantImage:   "docker.io/myorg/my-skill:v1.0.0",
		},
		{
			name:        "name is lowercased",
			skillName:   "My-Skill",
			dockerImage: "ghcr.io/org/skill:latest",
			version:     "2.0.0",
			wantName:    "my-skill",
			wantVer:     "2.0.0",
			wantImage:   "ghcr.io/org/skill:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dockerImageFlag = tt.dockerImage
			versionFlag = tt.version
			publishDesc = tt.desc

			skill, err := buildSkillDirectDocker(tt.skillName)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if skill.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", skill.Name, tt.wantName)
			}
			if skill.Version != tt.wantVer {
				t.Errorf("Version = %q, want %q", skill.Version, tt.wantVer)
			}
			if skill.Repository != nil {
				t.Error("Repository should be nil for Docker publish")
			}
			if len(skill.Packages) != 1 {
				t.Fatalf("Packages count = %d, want 1", len(skill.Packages))
			}
			pkg := skill.Packages[0]
			if pkg.Identifier != tt.wantImage {
				t.Errorf("Packages[0].Identifier = %q, want %q", pkg.Identifier, tt.wantImage)
			}
			if pkg.RegistryType != "docker" {
				t.Errorf("Packages[0].RegistryType = %q, want %q", pkg.RegistryType, "docker")
			}
			if pkg.Transport.Type != "docker" {
				t.Errorf("Packages[0].Transport.Type = %q, want %q", pkg.Transport.Type, "docker")
			}
		})
	}
}

func TestBuildSkillDirectDocker_MissingVersion(t *testing.T) {
	savePublishFlags(t)
	dockerImageFlag = "docker.io/myorg/my-skill:v1.0.0"
	versionFlag = ""

	_, err := buildSkillDirectDocker("my-skill")
	if err == nil {
		t.Fatal("expected error when --version is missing, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

func TestBuildSkillDirectDocker_MissingImage(t *testing.T) {
	savePublishFlags(t)
	dockerImageFlag = ""
	versionFlag = "1.0.0"

	_, err := buildSkillDirectDocker("my-skill")
	if err == nil {
		t.Fatal("expected error when --docker-image is missing, got nil")
	}
	if !contains(err.Error(), "--docker-image is required") {
		t.Errorf("error = %q, want it to contain '--docker-image is required'", err.Error())
	}
}

func TestRunPublish_DirectDockerSuccess(t *testing.T) {
	savePublishFlags(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v0/skills" { //nolint:nestif
			var skill models.SkillJSON
			if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
				t.Errorf("failed to decode request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if skill.Name != "docker-skill" {
				t.Errorf("skill name = %q, want %q", skill.Name, "docker-skill")
			}
			if skill.Version != "1.0.0" {
				t.Errorf("skill version = %q, want %q", skill.Version, "1.0.0")
			}
			if len(skill.Packages) != 1 {
				t.Errorf("packages count = %d, want 1", len(skill.Packages))
			} else if skill.Packages[0].Identifier != "docker.io/myorg/docker-skill:v1.0.0" {
				t.Errorf("package identifier = %q, want %q", skill.Packages[0].Identifier, "docker.io/myorg/docker-skill:v1.0.0")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	dockerImageFlag = "docker.io/myorg/docker-skill:v1.0.0"
	gitRepository = ""
	versionFlag = "1.0.0"
	dryRunFlag = false

	err := runPublish(nil, []string{"docker-skill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_DirectDockerDryRun(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	dockerImageFlag = "docker.io/myorg/my-skill:v1.0.0"
	gitRepository = ""
	versionFlag = "1.0.0"
	publishDesc = "test"
	dryRunFlag = true

	err := runPublish(nil, []string{"dry-run-docker"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_DockerImageWithFolder(t *testing.T) {
	savePublishFlags(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v0/skills" { //nolint:nestif
			var skill models.SkillJSON
			if err := json.NewDecoder(r.Body).Decode(&skill); err != nil {
				t.Errorf("failed to decode request body: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if skill.Name != "folder-docker-skill" {
				t.Errorf("skill name = %q, want %q", skill.Name, "folder-docker-skill")
			}
			if len(skill.Packages) != 1 {
				t.Errorf("packages count = %d, want 1", len(skill.Packages))
			} else if skill.Packages[0].Identifier != "docker.io/myorg/my-skill:v1.0.0" {
				t.Errorf("package identifier = %q, want %q", skill.Packages[0].Identifier, "docker.io/myorg/my-skill:v1.0.0")
			}
			if skill.Repository != nil {
				t.Error("repository should be nil for Docker publish")
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	dockerImageFlag = "docker.io/myorg/my-skill:v1.0.0"
	gitRepository = ""
	versionFlag = "1.0.0"
	dryRunFlag = false

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: folder-docker-skill\ndescription: from folder with docker\n---\n")

	err := runPublish(nil, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_FolderModeStillWorks(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var skill models.SkillJSON
		json.NewDecoder(r.Body).Decode(&skill)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	gitRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	dryRunFlag = false

	// Create a folder with SKILL.md — should use folder mode, not direct mode
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: folder-skill\ndescription: from folder\n---\n")

	err := runPublish(nil, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// --- checkGitHubSkillMdExists tests ---

func TestCheckGitHubSkillMdExists(t *testing.T) {
	tests := []struct {
		name        string
		ghURL       string
		serverCode  int
		wantErr     bool
		errContains string
	}{
		{
			name:       "SKILL.md exists at repo root",
			ghURL:      "https://github.com/org/repo",
			serverCode: http.StatusOK,
		},
		{
			name:       "SKILL.md exists at subpath",
			ghURL:      "https://github.com/org/repo/tree/main/skills/my-skill",
			serverCode: http.StatusOK,
		},
		{
			name:        "SKILL.md not found",
			ghURL:       "https://github.com/org/repo",
			serverCode:  http.StatusNotFound,
			wantErr:     true,
			errContains: "SKILL.md not found",
		},
		{
			name:        "server error",
			ghURL:       "https://github.com/org/repo",
			serverCode:  http.StatusInternalServerError,
			wantErr:     true,
			errContains: "HTTP 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.serverCode)
			}))
			t.Cleanup(srv.Close)

			origBaseURL := githubRawBaseURL
			githubRawBaseURL = srv.URL
			t.Cleanup(func() { githubRawBaseURL = origBaseURL })

			err := checkGitHubSkillMdExists(tt.ghURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("checkGitHubSkillMdExists() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.errContains)
				}
			}
		})
	}
}

func TestCheckGitHubSkillMdExists_InvalidURL(t *testing.T) {
	err := checkGitHubSkillMdExists("https://gitlab.com/org/repo")
	if err == nil {
		t.Fatal("expected error for non-GitHub URL, got nil")
	}
	if !contains(err.Error(), "unsupported host") {
		t.Errorf("error = %q, want it to contain 'unsupported host'", err.Error())
	}
}

func TestCheckGitHubSkillMdExists_VerifiesCorrectPath(t *testing.T) {
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	origBaseURL := githubRawBaseURL
	githubRawBaseURL = srv.URL
	t.Cleanup(func() { githubRawBaseURL = origBaseURL })

	tests := []struct {
		name     string
		ghURL    string
		wantPath string
	}{
		{
			name:     "repo root",
			ghURL:    "https://github.com/org/repo",
			wantPath: "/org/repo/HEAD/SKILL.md",
		},
		{
			name:     "with branch",
			ghURL:    "https://github.com/org/repo/tree/main",
			wantPath: "/org/repo/main/SKILL.md",
		},
		{
			name:     "with branch and subpath",
			ghURL:    "https://github.com/org/repo/tree/main/skills/my-skill",
			wantPath: "/org/repo/main/skills/my-skill/SKILL.md",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requestedPath = ""
			err := checkGitHubSkillMdExists(tt.ghURL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if requestedPath != tt.wantPath {
				t.Errorf("requested path = %q, want %q", requestedPath, tt.wantPath)
			}
		})
	}
}

// --- isValidSkillDir tests ---

func TestIsValidSkillDir(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dir string)
		want  bool
	}{
		{
			name: "valid SKILL.md with name and description",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: my-skill\ndescription: A test skill\n---\n# My Skill\n")
			},
			want: true,
		},
		{
			name: "SKILL.md with name only (missing description)",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: simple\n---\nBody.\n")
			},
			want: false,
		},
		{
			name: "SKILL.md with description only (missing name)",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "---\ndescription: no name\n---\nBody.\n")
			},
			want: false,
		},
		{
			name:  "no SKILL.md file",
			setup: func(dir string) {},
			want:  false,
		},
		{
			name: "other files but no SKILL.md",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "README.md"), "# README")
			},
			want: false,
		},
		{
			name: "empty SKILL.md",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "")
			},
			want: false,
		},
		{
			name: "SKILL.md without frontmatter delimiters",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "just some text\nno yaml here\n")
			},
			want: false,
		},
		{
			name: "SKILL.md with only opening delimiter",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: orphan\n")
			},
			want: false,
		},
		{
			name: "SKILL.md with invalid YAML in frontmatter",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: [invalid\n---\n")
			},
			want: false,
		},
		{
			name: "SKILL.md with empty frontmatter block",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "---\n---\nBody content.\n")
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			got := isValidSkillDir(dir)
			if got != tt.want {
				t.Errorf("isValidSkillDir() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsValidSkillDir_NonExistentDir(t *testing.T) {
	got := isValidSkillDir("/nonexistent/path/that/does/not/exist")
	if got {
		t.Error("isValidSkillDir() = true for non-existent directory, want false")
	}
}

// helpers

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}
