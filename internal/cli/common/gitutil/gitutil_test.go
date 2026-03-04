package gitutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
			name:    "non-tree segment ignored (e.g. blob)",
			rawURL:  "https://github.com/owner/repo/blob/main/README.md",
			wantURL: "https://github.com/owner/repo.git",
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
			gotURL, gotRef, gotPath, err := ParseGitHubURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseGitHubURL(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
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

func TestCopyRepoContents(t *testing.T) {
	t.Run("copies files and skips .git directory", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		os.MkdirAll(filepath.Join(repoDir, ".git", "objects"), 0o755)
		os.WriteFile(filepath.Join(repoDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0o644)
		os.WriteFile(filepath.Join(repoDir, "SKILL.md"), []byte("---\nname: test\n---\n"), 0o644)
		os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# readme"), 0o644)
		os.MkdirAll(filepath.Join(repoDir, "src"), 0o755)
		os.WriteFile(filepath.Join(repoDir, "src", "main.py"), []byte("print('hi')"), 0o644)

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(outDir, ".git")); !os.IsNotExist(err) {
			t.Error(".git directory should not be copied")
		}
		for _, rel := range []string{"SKILL.md", "README.md", "src/main.py"} {
			if _, err := os.Stat(filepath.Join(outDir, rel)); os.IsNotExist(err) {
				t.Errorf("expected %s to be copied", rel)
			}
		}

		got, _ := os.ReadFile(filepath.Join(outDir, "src", "main.py"))
		if string(got) != "print('hi')" {
			t.Errorf("main.py content = %q, want %q", string(got), "print('hi')")
		}
	})

	t.Run("navigates to subpath", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		os.MkdirAll(filepath.Join(repoDir, "skills", "my-skill"), 0o755)
		os.WriteFile(filepath.Join(repoDir, "skills", "my-skill", "SKILL.md"), []byte("skill"), 0o644)
		os.WriteFile(filepath.Join(repoDir, "skills", "my-skill", "config.yaml"), []byte("key: value"), 0o644)
		os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("root"), 0o644)

		if err := CopyRepoContents(repoDir, "skills/my-skill", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(outDir, "SKILL.md")); os.IsNotExist(err) {
			t.Error("expected SKILL.md from subpath")
		}
		if _, err := os.Stat(filepath.Join(outDir, "config.yaml")); os.IsNotExist(err) {
			t.Error("expected config.yaml from subpath")
		}
		if _, err := os.Stat(filepath.Join(outDir, "README.md")); !os.IsNotExist(err) {
			t.Error("root README.md should not be copied with subpath")
		}
	})

	t.Run("subpath not found returns error", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		err := CopyRepoContents(repoDir, "nonexistent/path", outDir)
		if err == nil {
			t.Fatal("expected error for missing subpath")
		}
	})

	t.Run("rejects dotdot traversal in subpath", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		err := CopyRepoContents(repoDir, "../../etc", outDir)
		if err == nil {
			t.Fatal("expected error for traversal subpath")
		}
		if !strings.Contains(err.Error(), "escapes repository") {
			t.Errorf("error = %q, want it to contain 'escapes repository'", err.Error())
		}
	})

	t.Run("rejects absolute subpath", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		err := CopyRepoContents(repoDir, "/etc/passwd", outDir)
		if err == nil {
			t.Fatal("expected error for absolute subpath")
		}
		if !strings.Contains(err.Error(), "must be relative") {
			t.Errorf("error = %q, want it to contain 'must be relative'", err.Error())
		}
	})

	t.Run("empty repo directory copies nothing", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		entries, _ := os.ReadDir(outDir)
		if len(entries) != 0 {
			t.Errorf("expected empty output, got %d entries", len(entries))
		}
	})

	t.Run("repo with only .git directory copies nothing", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755)
		os.WriteFile(filepath.Join(repoDir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0o644)

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		entries, _ := os.ReadDir(outDir)
		if len(entries) != 0 {
			t.Errorf("expected empty output (only .git should be skipped), got %d entries", len(entries))
		}
	})

	t.Run("deeply nested subpath", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		os.MkdirAll(filepath.Join(repoDir, "a", "b", "c"), 0o755)
		os.WriteFile(filepath.Join(repoDir, "a", "b", "c", "deep.txt"), []byte("deep"), 0o644)

		if err := CopyRepoContents(repoDir, "a/b/c", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
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

		scriptPath := filepath.Join(repoDir, "run.sh")
		os.WriteFile(scriptPath, []byte("#!/bin/sh\necho hi"), 0o755)

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		info, err := os.Stat(filepath.Join(outDir, "run.sh"))
		if err != nil {
			t.Fatalf("expected run.sh in output: %v", err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("run.sh permissions = %v, want %v", info.Mode().Perm(), os.FileMode(0o755))
		}
	})

	t.Run("copies mixed files and directories", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		os.WriteFile(filepath.Join(repoDir, "file1.txt"), []byte("one"), 0o644)
		os.WriteFile(filepath.Join(repoDir, "file2.txt"), []byte("two"), 0o644)
		os.MkdirAll(filepath.Join(repoDir, "dir1", "nested"), 0o755)
		os.WriteFile(filepath.Join(repoDir, "dir1", "nested", "inner.txt"), []byte("inner"), 0o644)
		os.MkdirAll(filepath.Join(repoDir, "dir2"), 0o755)
		os.WriteFile(filepath.Join(repoDir, "dir2", "other.txt"), []byte("other"), 0o644)
		os.MkdirAll(filepath.Join(repoDir, ".git"), 0o755)

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		for _, rel := range []string{"file1.txt", "file2.txt", "dir1/nested/inner.txt", "dir2/other.txt"} {
			if _, err := os.Stat(filepath.Join(outDir, rel)); os.IsNotExist(err) {
				t.Errorf("expected %s to be copied", rel)
			}
		}
		if _, err := os.Stat(filepath.Join(outDir, ".git")); !os.IsNotExist(err) {
			t.Error(".git should not be copied")
		}
	})

	t.Run("skips file symlinks", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		os.WriteFile(filepath.Join(repoDir, "real.txt"), []byte("real"), 0o644)
		os.Symlink(filepath.Join(repoDir, "real.txt"), filepath.Join(repoDir, "link.txt"))

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(outDir, "real.txt")); os.IsNotExist(err) {
			t.Error("expected real.txt to be copied")
		}
		if _, err := os.Lstat(filepath.Join(outDir, "link.txt")); !os.IsNotExist(err) {
			t.Error("expected link.txt (symlink) to be skipped")
		}
	})

	t.Run("skips directory symlinks", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		realDir := filepath.Join(repoDir, "real-dir")
		os.MkdirAll(realDir, 0o755)
		os.WriteFile(filepath.Join(realDir, "file.txt"), []byte("inside"), 0o644)
		os.Symlink(realDir, filepath.Join(repoDir, "link-dir"))

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(outDir, "real-dir", "file.txt")); os.IsNotExist(err) {
			t.Error("expected real-dir/file.txt to be copied")
		}
		if _, err := os.Lstat(filepath.Join(outDir, "link-dir")); !os.IsNotExist(err) {
			t.Error("expected link-dir (symlink) to be skipped")
		}
	})

	t.Run("skips symlinks pointing outside repo", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		os.WriteFile(filepath.Join(repoDir, "safe.txt"), []byte("safe"), 0o644)
		os.Symlink("/etc/hosts", filepath.Join(repoDir, "malicious-link"))

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(outDir, "safe.txt")); os.IsNotExist(err) {
			t.Error("expected safe.txt to be copied")
		}
		if _, err := os.Lstat(filepath.Join(outDir, "malicious-link")); !os.IsNotExist(err) {
			t.Error("expected malicious symlink to be skipped")
		}
	})

	t.Run("skips nested symlinks in subdirectories", func(t *testing.T) {
		repoDir := t.TempDir()
		outDir := filepath.Join(t.TempDir(), "output")

		subDir := filepath.Join(repoDir, "sub")
		os.MkdirAll(subDir, 0o755)
		os.WriteFile(filepath.Join(subDir, "real.txt"), []byte("real"), 0o644)
		os.Symlink("/etc/passwd", filepath.Join(subDir, "sneaky-link"))

		if err := CopyRepoContents(repoDir, "", outDir); err != nil {
			t.Fatalf("CopyRepoContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(outDir, "sub", "real.txt")); os.IsNotExist(err) {
			t.Error("expected sub/real.txt to be copied")
		}
		if _, err := os.Lstat(filepath.Join(outDir, "sub", "sneaky-link")); !os.IsNotExist(err) {
			t.Error("expected sub/sneaky-link (symlink) to be skipped")
		}
	})
}

func TestResolveSubPath(t *testing.T) {
	t.Run("valid subpath returns resolved directory", func(t *testing.T) {
		repoDir := t.TempDir()
		os.MkdirAll(filepath.Join(repoDir, "skills", "my-skill"), 0o755)

		got, err := resolveSubPath(repoDir, "skills/my-skill")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(repoDir, "skills", "my-skill")
		if got != want {
			t.Errorf("resolved = %q, want %q", got, want)
		}
	})

	t.Run("rejects absolute path", func(t *testing.T) {
		_, err := resolveSubPath(t.TempDir(), "/etc/passwd")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "must be relative") {
			t.Errorf("error = %q, want 'must be relative'", err.Error())
		}
	})

	t.Run("rejects dotdot traversal", func(t *testing.T) {
		_, err := resolveSubPath(t.TempDir(), "../../etc")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "escapes repository") {
			t.Errorf("error = %q, want 'escapes repository'", err.Error())
		}
	})

	t.Run("rejects nonexistent subpath", func(t *testing.T) {
		_, err := resolveSubPath(t.TempDir(), "does-not-exist")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "not found in repository") {
			t.Errorf("error = %q, want 'not found in repository'", err.Error())
		}
	})

	t.Run("cleans redundant path segments", func(t *testing.T) {
		repoDir := t.TempDir()
		os.MkdirAll(filepath.Join(repoDir, "a"), 0o755)

		got, err := resolveSubPath(repoDir, "a/./b/../")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(repoDir, "a")
		if got != want {
			t.Errorf("resolved = %q, want %q", got, want)
		}
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("copies content and preserves permissions", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()

		srcPath := filepath.Join(srcDir, "test.txt")
		dstPath := filepath.Join(dstDir, "test.txt")

		if err := os.WriteFile(srcPath, []byte("hello world"), 0o755); err != nil {
			t.Fatalf("write source: %v", err)
		}

		if err := CopyFile(srcPath, dstPath); err != nil {
			t.Fatalf("CopyFile() error = %v", err)
		}

		got, err := os.ReadFile(dstPath)
		if err != nil {
			t.Fatalf("read dest: %v", err)
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
		err := CopyFile("/nonexistent", filepath.Join(t.TempDir(), "out"))
		if err == nil {
			t.Fatal("expected error for missing source")
		}
	})

	t.Run("destination directory does not exist", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := t.TempDir()
		srcPath := filepath.Join(srcDir, "exists.txt")
		if err := os.WriteFile(srcPath, []byte("data"), 0o644); err != nil {
			t.Fatalf("write source: %v", err)
		}

		err := CopyFile(srcPath, filepath.Join(dstDir, "no", "such", "dir", "out.txt"))
		if err == nil {
			t.Fatal("expected error for missing dest directory")
		}
	})
}

func TestRepoNameFromCloneURL(t *testing.T) {
	tests := []struct {
		name     string
		cloneURL string
		want     string
	}{
		{"standard clone URL", "https://github.com/org/my-repo.git", "my-repo"},
		{"no .git suffix", "https://github.com/org/my-repo", "my-repo"},
		{"nested path", "https://github.com/deep/nested/repo-name.git", "repo-name"},
		{"dots and hyphens", "https://github.com/org/my-repo.v2.git", "my-repo.v2"},
		{"no slash", "my-repo.git", ""},
		{"empty string", "", ""},
		{"trailing slash stripped", "https://github.com/org/repo.git/", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RepoNameFromCloneURL(tt.cloneURL)
			if got != tt.want {
				t.Errorf("RepoNameFromCloneURL(%q) = %q, want %q", tt.cloneURL, got, tt.want)
			}
		})
	}
}

func TestCopyDir(t *testing.T) {
	t.Run("copies directory tree recursively", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "output")

		os.MkdirAll(filepath.Join(srcDir, "sub", "nested"), 0o755)
		os.WriteFile(filepath.Join(srcDir, "root.txt"), []byte("root"), 0o644)
		os.WriteFile(filepath.Join(srcDir, "sub", "file.txt"), []byte("sub"), 0o644)
		os.WriteFile(filepath.Join(srcDir, "sub", "nested", "deep.txt"), []byte("deep"), 0o644)

		if err := CopyDir(srcDir, dstDir); err != nil {
			t.Fatalf("CopyDir() error = %v", err)
		}

		for _, rel := range []string{"root.txt", "sub/file.txt", "sub/nested/deep.txt"} {
			if _, err := os.Stat(filepath.Join(dstDir, rel)); os.IsNotExist(err) {
				t.Errorf("expected %s to exist", rel)
			}
		}

		got, _ := os.ReadFile(filepath.Join(dstDir, "sub", "nested", "deep.txt"))
		if string(got) != "deep" {
			t.Errorf("deep.txt content = %q, want %q", string(got), "deep")
		}
	})

	t.Run("empty source directory", func(t *testing.T) {
		srcDir := t.TempDir()
		dstDir := filepath.Join(t.TempDir(), "output")

		if err := CopyDir(srcDir, dstDir); err != nil {
			t.Fatalf("CopyDir() error = %v", err)
		}

		entries, _ := os.ReadDir(dstDir)
		if len(entries) != 0 {
			t.Errorf("expected empty dir, got %d entries", len(entries))
		}
	})

	t.Run("source does not exist", func(t *testing.T) {
		err := CopyDir("/nonexistent/path", filepath.Join(t.TempDir(), "out"))
		if err == nil {
			t.Fatal("expected error for missing source")
		}
	})
}
