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
			name: "name only",
			content: `---
name: simple-skill
---
Body text.
`,
			wantName: "simple-skill",
			wantDesc: "",
		},
		{
			name: "empty name falls through",
			content: `---
description: no name provided
---
Body.
`,
			wantName: "",
			wantDesc: "no name provided",
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
	origGithub := githubRepository
	origVersion := versionFlag
	origGithubRawBase := githubRawBaseURL
	t.Cleanup(func() {
		githubRepository = origGithub
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
			name: "falls back to directory name when name is empty",
			skillMd: `---
description: No name
---
`,
			github:      "https://github.com/org/repo",
			version:     "2.0.0",
			wantVer:     "2.0.0",
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

			githubRepository = tt.github
			versionFlag = tt.version

			skill, err := buildSkillFromGitHub(dir)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantName != "" && skill.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", skill.Name, tt.wantName)
			}
			if tt.wantName == "" && skill.Name == "" {
				t.Error("expected Name to fall back to directory name, got empty")
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
			if skill.Repository.Source != "github" {
				t.Errorf("Repository.Source = %q, want %q", skill.Repository.Source, "github")
			}
			if len(skill.Packages) != 0 {
				t.Errorf("Packages should be empty for GitHub publish, got %d", len(skill.Packages))
			}
		})
	}
}

func TestBuildSkillFromGitHub_MissingVersion(t *testing.T) {
	origGithub := githubRepository
	origVersion := versionFlag
	t.Cleanup(func() {
		githubRepository = origGithub
		versionFlag = origVersion
	})

	githubRepository = "https://github.com/org/repo"
	versionFlag = ""

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test\n---\n")

	_, err := buildSkillFromGitHub(dir)
	if err == nil {
		t.Fatal("expected error when --version is missing for GitHub publish, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

func TestBuildSkillFromGitHub_InvalidFrontmatter(t *testing.T) {
	origGithub := githubRepository
	origVersion := versionFlag
	t.Cleanup(func() {
		githubRepository = origGithub
		versionFlag = origVersion
	})

	githubRepository = "https://github.com/org/repo"
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
	origGithub := githubRepository
	origVersion := versionFlag
	t.Cleanup(func() {
		githubRepository = origGithub
		versionFlag = origVersion
	})

	versionFlag = "1.0.0"

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test\n---\n")

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
			githubRepository = tt.github

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

func TestDetectSkills(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(dir string) // creates files/dirs under dir
		wantCount int
		wantErr   bool
	}{
		{
			name: "single skill in root",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: s\n---\n")
			},
			wantCount: 1,
		},
		{
			name: "multiple skills in subdirs",
			setup: func(dir string) {
				for _, name := range []string{"skill-a", "skill-b", "skill-c"} {
					sub := filepath.Join(dir, name)
					os.MkdirAll(sub, 0755)
					writeFile(t, filepath.Join(sub, "SKILL.md"), "---\nname: "+name+"\n---\n")
				}
			},
			wantCount: 3,
		},
		{
			name: "ignores subdirs without SKILL.md",
			setup: func(dir string) {
				sub := filepath.Join(dir, "has-skill")
				os.MkdirAll(sub, 0755)
				writeFile(t, filepath.Join(sub, "SKILL.md"), "---\nname: s\n---\n")

				noSkill := filepath.Join(dir, "no-skill")
				os.MkdirAll(noSkill, 0755)
				writeFile(t, filepath.Join(noSkill, "README.md"), "not a skill")
			},
			wantCount: 1,
		},
		{
			name: "no skills found",
			setup: func(dir string) {
				writeFile(t, filepath.Join(dir, "README.md"), "no skills here")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(dir)

			skills, err := detectSkills(dir)
			if (err != nil) != tt.wantErr {
				t.Fatalf("detectSkills() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(skills) != tt.wantCount {
				t.Errorf("got %d skills, want %d", len(skills), tt.wantCount)
			}
		})
	}
}

func TestBuildSkillDockerImage_NoDockerURL(t *testing.T) {
	origURL := dockerUrl
	origTag := tagFlag
	t.Cleanup(func() {
		dockerUrl = origURL
		tagFlag = origTag
	})

	dockerUrl = ""
	tagFlag = "1.0.0"

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test\n---\n")

	_, err := buildSkillDockerImage(dir)
	if err == nil {
		t.Fatal("expected error when docker url is empty, got nil")
	}
	if !contains(err.Error(), "docker url is required") {
		t.Errorf("error = %q, want it to contain 'docker url is required'", err.Error())
	}
}

// savePublishFlags saves all publish-related package-level vars and returns a cleanup function.
func savePublishFlags(t *testing.T) {
	t.Helper()
	origDockerUrl := dockerUrl
	origTagFlag := tagFlag
	origVersionFlag := versionFlag
	origPlatformFlag := platformFlag
	origPushFlag := pushFlag
	origDryRunFlag := dryRunFlag
	origGithubRepo := githubRepository
	origClient := apiClient
	origGithubRawBaseURL := githubRawBaseURL

	t.Cleanup(func() {
		dockerUrl = origDockerUrl
		tagFlag = origTagFlag
		versionFlag = origVersionFlag
		platformFlag = origPlatformFlag
		pushFlag = origPushFlag
		dryRunFlag = origDryRunFlag
		githubRepository = origGithubRepo
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
	githubRepository = "https://github.com/org/repo"

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
	githubRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"

	err := runPublish(nil, []string{"/nonexistent/path/to/skill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_DirWithoutSkillMdUsesDirectMode(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	var publishedName string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var skill models.SkillJSON
		json.NewDecoder(r.Body).Decode(&skill)
		publishedName = skill.Name
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	githubRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "no skill here")

	err := runPublish(nil, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Directory path treated as skill name via direct mode
	if publishedName == "" {
		t.Error("expected skill to be published in direct mode")
	}
}

func TestRunPublish_GitHubDryRun(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)
	apiClient = client.NewClient("http://localhost:0", "")
	githubRepository = "https://github.com/org/repo"
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
	githubRepository = "https://github.com/org/repo"
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
	githubRepository = "https://github.com/org/repo"
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

func TestRunPublish_GitHubMultipleSkills(t *testing.T) {
	savePublishFlags(t)
	mockGitHubSkillMdCheck(t)

	var publishedNames []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v0/skills" {
			var skill models.SkillJSON
			json.NewDecoder(r.Body).Decode(&skill)
			publishedNames = append(publishedNames, skill.Name)
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(models.SkillResponse{Skill: skill})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	apiClient = client.NewClient(srv.URL, "")
	githubRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	dryRunFlag = false

	// Create a parent dir with multiple skill subdirs
	dir := t.TempDir()
	for _, name := range []string{"skill-a", "skill-b"} {
		sub := filepath.Join(dir, name)
		os.MkdirAll(sub, 0755)
		writeFile(t, filepath.Join(sub, "SKILL.md"), "---\nname: "+name+"\ndescription: test\n---\n")
	}

	err := runPublish(nil, []string{dir})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(publishedNames) != 2 {
		t.Fatalf("expected 2 skills published, got %d: %v", len(publishedNames), publishedNames)
	}
}

func TestRunPublish_GitHubMissingVersion(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	githubRepository = "https://github.com/org/repo"
	versionFlag = ""
	dryRunFlag = false

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test\n---\n")

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
			githubRepository = tt.github
			versionFlag = tt.version
			publishDesc = tt.desc

			skill, err := buildSkillDirect(tt.skillName)
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
			if skill.Repository.Source != "github" {
				t.Errorf("Repository.Source = %q, want %q", skill.Repository.Source, "github")
			}
			if len(skill.Packages) != 0 {
				t.Errorf("Packages should be empty, got %d", len(skill.Packages))
			}
		})
	}
}

func TestBuildSkillDirect_MissingGithub(t *testing.T) {
	savePublishFlags(t)
	githubRepository = ""
	versionFlag = "1.0.0"

	_, err := buildSkillDirect("my-skill")
	if err == nil {
		t.Fatal("expected error when --github is missing, got nil")
	}
	if !contains(err.Error(), "--github is required") {
		t.Errorf("error = %q, want it to contain '--github is required'", err.Error())
	}
}

func TestBuildSkillDirect_MissingVersion(t *testing.T) {
	savePublishFlags(t)
	githubRepository = "https://github.com/org/repo"
	versionFlag = ""

	_, err := buildSkillDirect("my-skill")
	if err == nil {
		t.Fatal("expected error when --version is missing, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

func TestBuildSkillDirect_InvalidURL(t *testing.T) {
	savePublishFlags(t)
	githubRepository = "https://gitlab.com/org/repo"
	versionFlag = "1.0.0"

	_, err := buildSkillDirect("my-skill")
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
	githubRepository = "https://github.com/org/repo"
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
	githubRepository = "https://github.com/org/repo"
	versionFlag = "1.0.0"
	publishDesc = "test"
	dryRunFlag = true

	err := runPublish(nil, []string{"dry-run-direct"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunPublish_DirectMissingGithub(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	githubRepository = ""
	dockerUrl = ""
	versionFlag = "1.0.0"

	err := runPublish(nil, []string{"my-skill"})
	if err == nil {
		t.Fatal("expected error when neither flag is set, got nil")
	}
	if !contains(err.Error(), "--github is required") {
		t.Errorf("error = %q, want it to contain '--github is required'", err.Error())
	}
}

func TestRunPublish_DirectMissingVersion(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	githubRepository = "https://github.com/org/repo"
	versionFlag = ""

	err := runPublish(nil, []string{"my-skill"})
	if err == nil {
		t.Fatal("expected error when --version is missing in direct mode, got nil")
	}
	if !contains(err.Error(), "--version is required") {
		t.Errorf("error = %q, want it to contain '--version is required'", err.Error())
	}
}

func TestRunPublish_DockerUrlWithoutFolder(t *testing.T) {
	savePublishFlags(t)
	apiClient = client.NewClient("http://localhost:0", "")
	dockerUrl = "docker.io/myorg"
	githubRepository = ""

	err := runPublish(nil, []string{"not-a-folder"})
	if err == nil {
		t.Fatal("expected error when --docker-url is used without a skill folder, got nil")
	}
	if !contains(err.Error(), "--docker-url requires a local skill folder") {
		t.Errorf("error = %q, want it to contain '--docker-url requires a local skill folder'", err.Error())
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
	githubRepository = "https://github.com/org/repo"
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

func TestResolveDockerVersion(t *testing.T) {
	tests := []struct {
		name    string
		version string
		tag     string
		want    string
	}{
		{"version flag takes priority", "2.0.0", "latest", "2.0.0"},
		{"falls back to tag", "", "v1.0", "v1.0"},
		{"defaults to latest", "", "", "latest"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			origV := versionFlag
			origT := tagFlag
			t.Cleanup(func() {
				versionFlag = origV
				tagFlag = origT
			})

			versionFlag = tt.version
			tagFlag = tt.tag

			got := resolveDockerVersion()
			if got != tt.want {
				t.Errorf("resolveDockerVersion() = %q, want %q", got, tt.want)
			}
		})
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
