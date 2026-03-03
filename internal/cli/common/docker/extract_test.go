package docker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyNonEmptyContents(t *testing.T) {
	t.Run("copies non-empty files", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: test\n---\n"), 0644)
		os.WriteFile(filepath.Join(src, "config.yaml"), []byte("key: value"), 0644)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		for _, name := range []string{"SKILL.md", "config.yaml"} {
			if _, err := os.Stat(filepath.Join(dst, name)); os.IsNotExist(err) {
				t.Errorf("expected %s to be copied", name)
			}
		}
	})

	t.Run("skips empty files", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		os.WriteFile(filepath.Join(src, "nonempty.txt"), []byte("data"), 0644)
		os.WriteFile(filepath.Join(src, "empty.txt"), []byte(""), 0644)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(dst, "nonempty.txt")); os.IsNotExist(err) {
			t.Error("expected nonempty.txt to be copied")
		}
		if _, err := os.Stat(filepath.Join(dst, "empty.txt")); !os.IsNotExist(err) {
			t.Error("expected empty.txt to be skipped")
		}
	})

	t.Run("skips Docker system directories", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		// Create system dirs that Docker containers have
		for _, dir := range []string{"dev", "etc", "proc", "sys"} {
			os.MkdirAll(filepath.Join(src, dir), 0755)
			os.WriteFile(filepath.Join(src, dir, "some-file"), []byte("content"), 0644)
		}
		// Create a real skill file
		os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: test\n---\n"), 0644)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		// Skill file should be copied
		if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); os.IsNotExist(err) {
			t.Error("expected SKILL.md to be copied")
		}

		// System dirs should not be copied
		for _, dir := range []string{"dev", "etc", "proc", "sys"} {
			if _, err := os.Stat(filepath.Join(dst, dir)); !os.IsNotExist(err) {
				t.Errorf("expected %s to be skipped", dir)
			}
		}
	})

	t.Run("skips .dockerenv", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		os.WriteFile(filepath.Join(src, ".dockerenv"), []byte(""), 0644)
		os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("content"), 0644)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(dst, ".dockerenv")); !os.IsNotExist(err) {
			t.Error("expected .dockerenv to be skipped")
		}
		if _, err := os.Stat(filepath.Join(dst, "SKILL.md")); os.IsNotExist(err) {
			t.Error("expected SKILL.md to be copied")
		}
	})

	t.Run("skips directories with only empty files", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		os.MkdirAll(filepath.Join(src, "empty-dir"), 0755)
		os.WriteFile(filepath.Join(src, "empty-dir", "empty.txt"), []byte(""), 0644)

		os.MkdirAll(filepath.Join(src, "has-content"), 0755)
		os.WriteFile(filepath.Join(src, "has-content", "real.txt"), []byte("data"), 0644)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		if _, err := os.Stat(filepath.Join(dst, "empty-dir")); !os.IsNotExist(err) {
			t.Error("expected empty-dir to be skipped (contains only empty files)")
		}
		if _, err := os.Stat(filepath.Join(dst, "has-content", "real.txt")); os.IsNotExist(err) {
			t.Error("expected has-content/real.txt to be copied")
		}
	})

	t.Run("copies nested directory structure", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		os.MkdirAll(filepath.Join(src, "a", "b"), 0755)
		os.WriteFile(filepath.Join(src, "a", "b", "deep.txt"), []byte("deep"), 0644)
		os.WriteFile(filepath.Join(src, "root.txt"), []byte("root"), 0644)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		for _, rel := range []string{"root.txt", "a/b/deep.txt"} {
			path := filepath.Join(dst, rel)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Errorf("expected %s to be copied", rel)
			}
		}

		got, _ := os.ReadFile(filepath.Join(dst, "a", "b", "deep.txt"))
		if string(got) != "deep" {
			t.Errorf("deep.txt = %q, want %q", string(got), "deep")
		}
	})

	t.Run("preserves file permissions", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		scriptPath := filepath.Join(src, "run.sh")
		os.WriteFile(scriptPath, []byte("#!/bin/sh"), 0755)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		info, err := os.Stat(filepath.Join(dst, "run.sh"))
		if err != nil {
			t.Fatalf("expected run.sh to be copied: %v", err)
		}
		if info.Mode().Perm() != 0755 {
			t.Errorf("permissions = %v, want %v", info.Mode().Perm(), os.FileMode(0755))
		}
	})

	t.Run("empty source directory", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		entries, _ := os.ReadDir(dst)
		if len(entries) != 0 {
			t.Errorf("expected empty output, got %d entries", len(entries))
		}
	})

	t.Run("simulates full Docker extraction", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		// Simulate a typical Docker container filesystem extraction
		// System dirs (should be skipped)
		for _, dir := range []string{"dev", "etc", "proc", "sys"} {
			os.MkdirAll(filepath.Join(src, dir), 0755)
			os.WriteFile(filepath.Join(src, dir, "placeholder"), []byte("sys"), 0644)
		}
		os.WriteFile(filepath.Join(src, ".dockerenv"), []byte(""), 0644)

		// Actual skill content (should be copied)
		os.WriteFile(filepath.Join(src, "SKILL.md"), []byte("---\nname: my-skill\n---\n"), 0644)
		os.WriteFile(filepath.Join(src, "prompt.txt"), []byte("You are a helpful assistant"), 0644)
		os.MkdirAll(filepath.Join(src, "tools"), 0755)
		os.WriteFile(filepath.Join(src, "tools", "helper.py"), []byte("def help(): pass"), 0644)

		// Empty files and dirs (should be skipped)
		os.WriteFile(filepath.Join(src, "empty-marker"), []byte(""), 0644)
		os.MkdirAll(filepath.Join(src, "empty-subdir"), 0755)

		if err := CopyNonEmptyContents(src, dst); err != nil {
			t.Fatalf("CopyNonEmptyContents() error = %v", err)
		}

		// Should be present
		for _, rel := range []string{"SKILL.md", "prompt.txt", "tools/helper.py"} {
			if _, err := os.Stat(filepath.Join(dst, rel)); os.IsNotExist(err) {
				t.Errorf("expected %s to be copied", rel)
			}
		}

		// Should NOT be present
		for _, rel := range []string{".dockerenv", "dev", "etc", "proc", "sys", "empty-marker", "empty-subdir"} {
			if _, err := os.Stat(filepath.Join(dst, rel)); !os.IsNotExist(err) {
				t.Errorf("expected %s to be skipped", rel)
			}
		}
	})
}

func TestCopyFile(t *testing.T) {
	t.Run("copies content and permissions", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		srcPath := filepath.Join(src, "test.txt")
		dstPath := filepath.Join(dst, "test.txt")
		os.WriteFile(srcPath, []byte("hello"), 0755)

		if err := CopyFile(srcPath, dstPath); err != nil {
			t.Fatalf("CopyFile() error = %v", err)
		}

		got, _ := os.ReadFile(dstPath)
		if string(got) != "hello" {
			t.Errorf("content = %q, want %q", string(got), "hello")
		}

		info, _ := os.Stat(dstPath)
		if info.Mode().Perm() != 0755 {
			t.Errorf("permissions = %v, want %v", info.Mode().Perm(), os.FileMode(0755))
		}
	})

	t.Run("creates parent directories", func(t *testing.T) {
		src := t.TempDir()
		dst := t.TempDir()

		srcPath := filepath.Join(src, "file.txt")
		dstPath := filepath.Join(dst, "a", "b", "c", "file.txt")
		os.WriteFile(srcPath, []byte("nested"), 0644)

		if err := CopyFile(srcPath, dstPath); err != nil {
			t.Fatalf("CopyFile() error = %v", err)
		}

		got, _ := os.ReadFile(dstPath)
		if string(got) != "nested" {
			t.Errorf("content = %q, want %q", string(got), "nested")
		}
	})

	t.Run("source not found", func(t *testing.T) {
		dst := t.TempDir()
		err := CopyFile("/nonexistent/file.txt", filepath.Join(dst, "out.txt"))
		if err == nil {
			t.Fatal("expected error for missing source, got nil")
		}
	})
}
