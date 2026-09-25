package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestFromDir(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, ".claude-plugin/plugin.json", `{"name":"deploy"}`)
	writeFile(t, root, "skills/deploy/SKILL.md", "---\nname: deploy\n---\n")
	writeFile(t, root, "skills/deploy/reference.md", "supporting")
	writeFile(t, root, "bin/tool", "#!/bin/sh\n")
	// A .git directory must be skipped wholesale.
	writeFile(t, root, ".git/config", "[core]\n")
	// An empty directory holds no file, so only Dirs can show it.
	if err := os.Mkdir(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	b, err := FromDir(root)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	want := map[string][]byte{
		".claude-plugin/plugin.json": []byte(`{"name":"deploy"}`),
		"skills/deploy/SKILL.md":     []byte("---\nname: deploy\n---\n"),
		"skills/deploy/reference.md": []byte("supporting"),
		"bin/tool":                   []byte("#!/bin/sh\n"),
	}
	if !reflect.DeepEqual(b.Files, want) {
		t.Fatalf("FromDir files mismatch:\n got  %v\n want %v", b.Files, want)
	}
	wantDirs := map[string]bool{".claude-plugin": true, "skills": true, "skills/deploy": true, "bin": true, "empty": true}
	if !reflect.DeepEqual(b.Dirs, wantDirs) {
		t.Fatalf("FromDir dirs mismatch:\n got  %v\n want %v", b.Dirs, wantDirs)
	}
}

func TestFromDirSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "real.txt", "ok")
	// A symlink pointing outside the tree must not be followed or copied.
	if err := os.Symlink("/etc/passwd", filepath.Join(root, "evil")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	b, err := FromDir(root)
	if err != nil {
		t.Fatalf("FromDir: %v", err)
	}
	if _, ok := b.Files["evil"]; ok {
		t.Fatal("symlink should have been skipped")
	}
	if string(b.Files["real.txt"]) != "ok" {
		t.Fatalf("real file missing: %v", b.Files)
	}
}

func TestFromDirRejectsBackslashDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, `a\b`), 0o755); err != nil {
		t.Skipf("backslash directory name unsupported: %v", err)
	}
	if _, err := FromDir(root); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected ErrInvalidBundle, got %v", err)
	}
}

func TestValidateBundlePathTraversal(t *testing.T) {
	for _, p := range []string{"", "../evil", "/abs", "a/../../b", "a\\b", "a/./b"} {
		if err := validateBundlePath(p); !errors.Is(err, ErrInvalidBundle) {
			t.Fatalf("path %q: expected ErrInvalidBundle, got %v", p, err)
		}
	}
	for _, p := range []string{"a", "a/b/c.md", ".claude-plugin/plugin.json"} {
		if err := validateBundlePath(p); err != nil {
			t.Fatalf("path %q: unexpected error %v", p, err)
		}
	}
}

func TestFromDirRejectsTooManyFiles(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		writeFile(t, root, n+".txt", "x")
	}
	if _, err := fromDir(root, 2, MaxBundleBytes); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected ErrInvalidBundle for file-count overflow, got %v", err)
	}
}

func TestFromDirRejectsTooManyBytes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "big.txt", "0123456789") // 10 bytes
	if _, err := fromDir(root, MaxBundleFiles, 5); !errors.Is(err, ErrInvalidBundle) {
		t.Fatalf("expected ErrInvalidBundle for byte overflow, got %v", err)
	}
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
