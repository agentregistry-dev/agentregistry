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
			name:     "trailing slash on repo URL",
			rawURL:   "https://github.com/owner/repo/",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "",
			wantPath: "",
		},
		{
			name:     "non-tree segment ignored (e.g. blob)",
			rawURL:   "https://github.com/owner/repo/blob/main/README.md",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "",
			wantPath: "",
		},
		{
			name:     "three path segments without tree",
			rawURL:   "https://github.com/owner/repo/issues",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "",
			wantPath: "",
		},
		{
			name:     "repo name with dots and hyphens",
			rawURL:   "https://github.com/my-org/my-repo.v2",
			wantURL:  "https://github.com/my-org/my-repo.v2.git",
			wantRef:  "",
			wantPath: "",
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
			name:     "repo URL ending with .git",
			rawURL:   "https://github.com/owner/repo.git",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "",
			wantPath: "",
		},
		{
			name:     "repo URL with .git and tree path",
			rawURL:   "https://github.com/owner/repo.git/tree/main/src",
			wantURL:  "https://github.com/owner/repo.git",
			wantRef:  "main",
			wantPath: "src",
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
			name:    "empty path",
			rawURL:  "https://github.com",
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

func TestCopyFile(t *testing.T) {
	srcDir := t.TempDir()
	dstDir := t.TempDir()

	t.Run("copies content and preserves permissions", func(t *testing.T) {
		srcPath := filepath.Join(srcDir, "test.txt")
		dstPath := filepath.Join(dstDir, "test.txt")

		if err := os.WriteFile(srcPath, []byte("hello world"), 0755); err != nil {
			t.Fatalf("failed to write source file: %v", err)
		}

		if err := copyFile(srcPath, dstPath); err != nil {
			t.Fatalf("copyFile() error = %v", err)
		}

		got, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("failed to read destination file: %v", err)
		}
		if string(got) != "hello world" {
			t.Errorf("content = %q, want %q", string(got), "hello world")
		}

		srcInfo, _ := os.Stat(srcPath)
		dstInfo, _ := os.Stat(dstPath)
		if srcInfo.Mode() != dstInfo.Mode() {
			t.Errorf("mode = %v, want %v", dstInfo.Mode(), srcInfo.Mode())
		}
	})

	t.Run("source does not exist", func(t *testing.T) {
		err := copyFile(filepath.Join(srcDir, "nonexistent"), filepath.Join(dstDir, "out"))
		if err == nil {
			t.Fatal("expected error for missing source, got nil")
		}
	})

	t.Run("destination directory does not exist", func(t *testing.T) {
		srcPath := filepath.Join(srcDir, "exists.txt")
		if err := os.WriteFile(srcPath, []byte("data"), 0644); err != nil {
			t.Fatalf("failed to write source: %v", err)
		}

		err := copyFile(srcPath, filepath.Join(dstDir, "no", "such", "dir", "out.txt"))
		if err == nil {
			t.Fatal("expected error for missing dest directory, got nil")
		}
	})
}

func TestCopyDir(t *testing.T) {
	t.Run("copies directory tree recursively", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "output")

		// Create a nested structure
		os.MkdirAll(filepath.Join(srcDir, "sub", "nested"), 0755)
		os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root"), 0644)
		os.WriteFile(filepath.Join(srcDir, "sub", "file.txt"), []byte("sub"), 0644)
		os.WriteFile(filepath.Join(srcDir, "sub", "nested", "deep.txt"), []byte("deep"), 0644)

		if err := copyDir(srcDir, dstDir); err != nil {
			t.Fatalf("copyDir() error = %v", err)
		}

		// Verify all files were copied
		for _, rel := range []string{"root.txt", "sub/file.txt", "sub/nested/deep.txt"} {
			path := filepath.Join(dstDir, rel)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("expected file %s to exist", rel)
			}
		}

		// Verify content
		got, _ := os.ReadFile(filepath.Join(dstDir, "sub", "nested", "deep.txt"))
		if string(got) != "deep" {
			t.Errorf("deep.txt content = %q, want %q", string(got), "deep")
		}
	})

	t.Run("empty source directory", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "output")

		if err := copyDir(srcDir, dstDir); err != nil {
			t.Fatalf("copyDir() error = %v", err)
		}

		entries, _ := os.ReadDir(dstDir)
		if len(entries) != 0 {
			t.Errorf("expected empty dir, got %d entries", len(entries))
		}
	})

	t.Run("source does not exist", func(t *testing.T) {
		err := copyDir("/nonexistent/path", filepath.Join(t.TempDir(), "out"))
		if err == nil {
			t.Fatal("expected error for missing source, got nil")
		}
	})
}

// newTestServer creates an httptest server that serves skill API responses.
// The handler map keys are URL paths, values are the response to return.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, *client.Client) {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.NewClient(srv.URL, "")
	return srv, c
}

func jsonResponse(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("failed to encode JSON response: %v", err)
	}
}

func TestResolveSkillVersion(t *testing.T) {
	t.Run("explicit version returns immediately", func(t *testing.T) {
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		// apiClient doesn't need to be set when version is explicit
		apiClient = nil

		v, err := resolveSkillVersion("my-skill", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "1.0.0" {
			t.Errorf("version = %q, want %q", v, "1.0.0")
		}
	})

	t.Run("single version auto-selected", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/my-skill/versions": func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, models.SkillListResponse{
					Skills: []models.SkillResponse{
						{Skill: models.SkillJSON{Name: "my-skill", Version: "2.0.0"}},
					},
				})
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		v, err := resolveSkillVersion("my-skill", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "2.0.0" {
			t.Errorf("version = %q, want %q", v, "2.0.0")
		}
	})

	t.Run("multiple versions requires explicit selection", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/my-skill/versions": func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, models.SkillListResponse{
					Skills: []models.SkillResponse{
						{Skill: models.SkillJSON{Name: "my-skill", Version: "1.0.0"}},
						{Skill: models.SkillJSON{Name: "my-skill", Version: "2.0.0"}},
					},
				})
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		_, err := resolveSkillVersion("my-skill", "")
		if err == nil {
			t.Fatal("expected error for multiple versions, got nil")
		}
		if got := err.Error(); !stringContains(got, "multiple versions") {
			t.Errorf("error = %q, want it to contain 'multiple versions'", got)
		}
	})

	t.Run("no versions found", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/unknown/versions": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		_, err := resolveSkillVersion("unknown", "")
		if err == nil {
			t.Fatal("expected error for unknown skill, got nil")
		}
		if got := err.Error(); !stringContains(got, "not found") {
			t.Errorf("error = %q, want it to contain 'not found'", got)
		}
	})

	t.Run("API error propagated", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/broken/versions": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		_, err := resolveSkillVersion("broken", "")
		if err == nil {
			t.Fatal("expected error for API failure, got nil")
		}
	})
}

func TestRunPull_NilClient(t *testing.T) {
	origClient := apiClient
	t.Cleanup(func() { apiClient = origClient })
	apiClient = nil

	err := runPull(nil, []string{"some-skill"})
	if err == nil {
		t.Fatal("expected error for nil apiClient, got nil")
	}
	if got := err.Error(); !stringContains(got, "API client not initialized") {
		t.Errorf("error = %q, want it to contain 'API client not initialized'", got)
	}
}

func TestRunPull_SkillNotFound(t *testing.T) {
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/nonexistent/versions": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	origClient := apiClient
	t.Cleanup(func() { apiClient = origClient })
	apiClient = c

	err := runPull(nil, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
}

func TestRunPull_NoSourceAvailable(t *testing.T) {
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/no-source/versions": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillListResponse{
				Skills: []models.SkillResponse{
					{Skill: models.SkillJSON{Name: "no-source", Version: "1.0.0"}},
				},
			})
		},
		"/skills/no-source/versions/1.0.0": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillResponse{
				Skill: models.SkillJSON{
					Name:    "no-source",
					Version: "1.0.0",
					// No packages, no repository
				},
			})
		},
	})
	origClient := apiClient
	origVersion := pullVersion
	t.Cleanup(func() {
		apiClient = origClient
		pullVersion = origVersion
	})
	apiClient = c
	pullVersion = ""

	err := runPull(nil, []string{"no-source"})
	if err == nil {
		t.Fatal("expected error for skill with no source, got nil")
	}
	if got := err.Error(); !stringContains(got, "no Docker package or GitHub repository") {
		t.Errorf("error = %q, want it to contain 'no Docker package or GitHub repository'", got)
	}
}

func TestRunPull_OutputDirDefault(t *testing.T) {
	// Verify the default output directory is "skills/<name>"
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/myskill/versions": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillListResponse{
				Skills: []models.SkillResponse{
					{Skill: models.SkillJSON{Name: "myskill", Version: "1.0.0"}},
				},
			})
		},
		"/skills/myskill/versions/1.0.0": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillResponse{
				Skill: models.SkillJSON{
					Name:    "myskill",
					Version: "1.0.0",
					// No sources - will fail, but we check the output dir was created
				},
			})
		},
	})
	origClient := apiClient
	origVersion := pullVersion
	origDir, _ := os.Getwd()
	t.Cleanup(func() {
		apiClient = origClient
		pullVersion = origVersion
		os.Chdir(origDir)
	})
	apiClient = c
	pullVersion = ""

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	// Will fail because no source, but output dir should be created
	_ = runPull(nil, []string{"myskill"})

	expectedDir := filepath.Join(tmpDir, "skills", "myskill")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expected default output directory %s to be created", expectedDir)
	}
}

func TestRunPull_CustomOutputDir(t *testing.T) {
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/myskill/versions": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillListResponse{
				Skills: []models.SkillResponse{
					{Skill: models.SkillJSON{Name: "myskill", Version: "1.0.0"}},
				},
			})
		},
		"/skills/myskill/versions/1.0.0": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillResponse{
				Skill: models.SkillJSON{
					Name:    "myskill",
					Version: "1.0.0",
				},
			})
		},
	})
	origClient := apiClient
	origVersion := pullVersion
	t.Cleanup(func() {
		apiClient = origClient
		pullVersion = origVersion
	})
	apiClient = c
	pullVersion = ""

	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "my-custom-output")

	// Will fail because no source, but custom output dir should be created
	_ = runPull(nil, []string{"myskill", customDir})

	if _, err := os.Stat(customDir); os.IsNotExist(err) {
		t.Errorf("expected custom output directory %s to be created", customDir)
	}
}

func TestCopyRepoContents(t *testing.T) {
	t.Run("copies files and skips .git directory", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		// Simulate a cloned repo with .git, files, and subdirectories
		os.MkdirAll(filepath.Join(repoDir, ".git", "objects"), 0755)
		os.WriteFile(filepath.Join(repoDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644)
		os.WriteFile(filepath.Join(repoDir, "SKILL.md"), []byte("---\nname: test\n---\n"), 0644)
		os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# readme"), 0644)
		os.MkdirAll(filepath.Join(repoDir, "src"), 0755)
		os.WriteFile(filepath.Join(repoDir, "src", "main.py"), []byte("print('hi')"), 0644)

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		// .git must NOT be copied
		if _, err := os.Stat(filepath.Join(outDir, ".git")); !os.IsNotExist(err) {
			t.Error(".git directory should not be copied to output")
		}

		// Other files must be copied
		for _, rel := range []string{"SKILL.md", "README.md", "src/main.py"} {
			if _, err := os.Stat(filepath.Join(outDir, rel)); os.IsNotExist(err) {
				t.Errorf("expected %s to be copied", rel)
			}
		}

		// Verify content
		got, _ := os.ReadFile(filepath.Join(outDir, "src", "main.py"))
		if string(got) != "print('hi')" {
			t.Errorf("main.py content = %q, want %q", string(got), "print('hi')")
		}
	})

	t.Run("navigates to subpath", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		// Create nested structure
		os.MkdirAll(filepath.Join(repoDir, "skills", "my-skill"), 0755)
		os.WriteFile(filepath.Join(repoDir, "skills", "my-skill", "SKILL.md"), []byte("---\nname: nested\n---\n"), 0644)
		os.WriteFile(filepath.Join(repoDir, "skills", "my-skill", "config.yaml"), []byte("key: value"), 0644)
		// Root-level file should NOT be copied
		os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("root readme"), 0644)

		if err := copyRepoContents(repoDir, "skills/my-skill", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		// Files from subpath should be in output
		if _, err := os.Stat(filepath.Join(outDir, "SKILL.md")); os.IsNotExist(err) {
			t.Error("expected SKILL.md from subpath to be copied")
		}
		if _, err := os.Stat(filepath.Join(outDir, "config.yaml")); os.IsNotExist(err) {
			t.Error("expected config.yaml from subpath to be copied")
		}

		// Root-level file should NOT be in output
		if _, err := os.Stat(filepath.Join(outDir, "README.md")); !os.IsNotExist(err) {
			t.Error("root README.md should not be copied when subpath is specified")
		}
	})

	t.Run("subpath not found returns error", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		err := copyRepoContents(repoDir, "nonexistent/path", outDir)
		if err == nil {
			t.Fatal("expected error for missing subpath, got nil")
		}
		if !stringContains(err.Error(), "not found in repository") {
			t.Errorf("error = %q, want it to contain 'not found in repository'", err.Error())
		}
	})

	t.Run("empty repo directory copies nothing", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		entries, _ := os.ReadDir(outDir)
		if len(entries) != 0 {
			t.Errorf("expected empty output, got %d entries", len(entries))
		}
	})

	t.Run("repo with only .git directory copies nothing", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		os.MkdirAll(filepath.Join(repoDir, ".git"), 0755)
		os.WriteFile(filepath.Join(repoDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644)

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		entries, _ := os.ReadDir(outDir)
		if len(entries) != 0 {
			t.Errorf("expected empty output (only .git should be skipped), got %d entries", len(entries))
		}
	})

	t.Run("deeply nested subpath", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		os.MkdirAll(filepath.Join(repoDir, "a", "b", "c"), 0755)
		os.WriteFile(filepath.Join(repoDir, "a", "b", "c", "deep.txt"), []byte("deep"), 0644)

		if err := copyRepoContents(repoDir, "a/b/c", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		got, err := os.ReadFile(filepath.Join(outDir, "deep.txt"))
		if err != nil {
			t.Fatalf("expected deep.txt in output: %v", err)
		}
		if string(got) != "deep" {
			t.Errorf("deep.txt = %q, want %q", string(got), "deep")
		}
	})

	t.Run("preserves file permissions", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		scriptPath := filepath.Join(repoDir, "run.sh")
		os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi"), 0755)

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		info, err := os.Stat(filepath.Join(outDir, "run.sh"))
		if err != nil {
			t.Fatalf("expected run.sh in output: %v", err)
		}
		if info.Mode().Perm() != 0755 {
			t.Errorf("run.sh permissions = %v, want %v", info.Mode().Perm(), os.FileMode(0755))
		}
	})

	t.Run("copies mixed files and directories", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		// Mix of files and dirs at root level
		os.WriteFile(filepath.Join(repoDir, "file1.txt"), []byte("one"), 0644)
		os.WriteFile(filepath.Join(repoDir, "file2.txt"), []byte("two"), 0644)
		os.MkdirAll(filepath.Join(repoDir, "dir1", "nested"), 0755)
		os.WriteFile(filepath.Join(repoDir, "dir1", "nested", "inner.txt"), []byte("inner"), 0644)
		os.MkdirAll(filepath.Join(repoDir, "dir2"), 0755)
		os.WriteFile(filepath.Join(repoDir, "dir2", "other.txt"), []byte("other"), 0644)
		// .git should be skipped
		os.MkdirAll(filepath.Join(repoDir, ".git"), 0755)

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		expected := []string{
			"file1.txt",
			"file2.txt",
			"dir1/nested/inner.txt",
			"dir2/other.txt",
		}
		for _, rel := range expected {
			if _, err := os.Stat(filepath.Join(outDir, rel)); os.IsNotExist(err) {
				t.Errorf("expected %s to be copied", rel)
			}
		}

		// .git must not be present
		if _, err := os.Stat(filepath.Join(outDir, ".git")); !os.IsNotExist(err) {
			t.Error(".git should not be copied")
		}
	})

	t.Run("skips file symlinks", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		// Create a real file and a symlink to it
		os.WriteFile(filepath.Join(repoDir, "real.txt"), []byte("real"), 0644)
		os.Symlink(filepath.Join(repoDir, "real.txt"), filepath.Join(repoDir, "link.txt"))

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		// Real file should be copied
		if _, err := os.Stat(filepath.Join(outDir, "real.txt")); os.IsNotExist(err) {
			t.Error("expected real.txt to be copied")
		}
		// Symlink should be skipped
		if _, err := os.Lstat(filepath.Join(outDir, "link.txt")); !os.IsNotExist(err) {
			t.Error("expected link.txt (symlink) to be skipped")
		}
	})

	t.Run("skips directory symlinks", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		// Create a real dir and a symlink to it
		realDir := filepath.Join(repoDir, "real-dir")
		os.MkdirAll(realDir, 0755)
		os.WriteFile(filepath.Join(realDir, "file.txt"), []byte("inside"), 0644)
		os.Symlink(realDir, filepath.Join(repoDir, "link-dir"))

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		// Real dir should be copied
		if _, err := os.Stat(filepath.Join(outDir, "real-dir", "file.txt")); os.IsNotExist(err) {
			t.Error("expected real-dir/file.txt to be copied")
		}
		// Symlinked dir should be skipped
		if _, err := os.Lstat(filepath.Join(outDir, "link-dir")); !os.IsNotExist(err) {
			t.Error("expected link-dir (symlink) to be skipped")
		}
	})

	t.Run("skips symlinks pointing outside repo", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		// Create a symlink pointing to an absolute path outside the repo
		os.WriteFile(filepath.Join(repoDir, "safe.txt"), []byte("safe"), 0644)
		os.Symlink("/etc/hosts", filepath.Join(repoDir, "malicious-link"))

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		// Safe file should be copied
		if _, err := os.Stat(filepath.Join(outDir, "safe.txt")); os.IsNotExist(err) {
			t.Error("expected safe.txt to be copied")
		}
		// Malicious symlink should be skipped
		if _, err := os.Lstat(filepath.Join(outDir, "malicious-link")); !os.IsNotExist(err) {
			t.Error("expected malicious symlink to be skipped")
		}
	})

	t.Run("skips nested symlinks in subdirectories", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")
		os.MkdirAll(outDir, 0755)

		// Create a nested structure with a symlink inside a subdirectory
		subDir := filepath.Join(repoDir, "sub")
		os.MkdirAll(subDir, 0755)
		os.WriteFile(filepath.Join(subDir, "real.txt"), []byte("real"), 0644)
		os.Symlink("/etc/passwd", filepath.Join(subDir, "sneaky-link"))

		if err := copyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("copyRepoContents() error = %v", err)
		}

		// Real file in subdirectory should be copied
		if _, err := os.Stat(filepath.Join(outDir, "sub", "real.txt")); os.IsNotExist(err) {
			t.Error("expected sub/real.txt to be copied")
		}
		// Nested symlink should be skipped by copyDir
		if _, err := os.Lstat(filepath.Join(outDir, "sub", "sneaky-link")); !os.IsNotExist(err) {
			t.Error("expected sub/sneaky-link (symlink) to be skipped")
		}
	})
}

// stringContains is a simple helper to check substring presence.
func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
