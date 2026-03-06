package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunSkillBuild_MissingPath(t *testing.T) {
	err := runSkillBuild(nil, []string{"/nonexistent/path"})
	if err == nil {
		t.Fatal("expected error for nonexistent path, got nil")
	}
}

func TestRunSkillBuild_NotADirectory(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "notadir")
	if err != nil {
		t.Fatal(err)
	}
	f.Close()

	err = runSkillBuild(nil, []string{f.Name()})
	if err == nil {
		t.Fatal("expected error for non-directory path, got nil")
	}
}

func TestRunSkillBuild_MissingSkillMd(t *testing.T) {
	dir := t.TempDir()
	err := runSkillBuild(nil, []string{dir})
	if err == nil {
		t.Fatal("expected error for missing SKILL.md, got nil")
	}
}

func TestRunSkillBuild_ValidSkillMd(t *testing.T) {
	dir := t.TempDir()
	skillMd := `---
name: test-skill
description: A test skill
---
# Test Skill
`
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMd), 0o644); err != nil {
		t.Fatal(err)
	}

	// This will fail because Docker isn't available in test, but it should
	// get past the metadata parsing stage
	err := runSkillBuild(nil, []string{dir})
	if err == nil {
		// Docker might actually be available; that's OK too
		return
	}
	// Should fail at Docker build stage, not at metadata parsing
	if err.Error() == "failed to read skill metadata" {
		t.Errorf("should have parsed SKILL.md successfully, got: %v", err)
	}
}
