package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func saveBuildFlags(t *testing.T) {
	t.Helper()
	origImage := buildImage
	origPush := buildPush
	origPlatform := buildPlatform

	t.Cleanup(func() {
		buildImage = origImage
		buildPush = origPush
		buildPlatform = origPlatform
	})
}

func TestRunBuild_InvalidDir(t *testing.T) {
	saveBuildFlags(t)
	buildImage = "test:latest"

	err := runBuild(nil, []string{"/nonexistent/path"})
	if err == nil {
		t.Fatal("expected error for nonexistent directory, got nil")
	}
	if !contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, want it to contain 'does not exist'", err.Error())
	}
}

func TestRunBuild_NoSkillsFound(t *testing.T) {
	saveBuildFlags(t)
	buildImage = "test:latest"

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "README.md"), "no skills here")

	err := runBuild(nil, []string{dir})
	if err == nil {
		t.Fatal("expected error when no skills found, got nil")
	}
	if !contains(err.Error(), "failed to detect skills") {
		t.Errorf("error = %q, want it to contain 'failed to detect skills'", err.Error())
	}
}

func TestBuildSkillImage_MissingImageFlag(t *testing.T) {
	saveBuildFlags(t)
	buildImage = ""

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: test-skill\n---\n")

	err := buildSkillImage(dir, nil)
	if err == nil {
		t.Fatal("expected error when --image is not set, got nil")
	}
	if !contains(err.Error(), "--image is required") {
		t.Errorf("error = %q, want it to contain '--image is required'", err.Error())
	}
}

func TestBuildSkillImage_InvalidFrontmatter(t *testing.T) {
	saveBuildFlags(t)
	buildImage = "test:latest"

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "SKILL.md"), "no frontmatter here")

	err := buildSkillImage(dir, nil)
	if err == nil {
		t.Fatal("expected error for invalid frontmatter, got nil")
	}
	if !contains(err.Error(), "failed to resolve skill metadata") {
		t.Errorf("error = %q, want it to contain 'failed to resolve skill metadata'", err.Error())
	}
}

func TestRunBuild_DetectsMultipleSkills(t *testing.T) {
	saveBuildFlags(t)
	buildImage = "test:latest"

	dir := t.TempDir()
	for _, name := range []string{"skill-a", "skill-b"} {
		sub := filepath.Join(dir, name)
		os.MkdirAll(sub, 0755)
		writeFile(t, filepath.Join(sub, "SKILL.md"), "---\nname: "+name+"\n---\n")
	}

	// This will fail at docker check (docker not available in test),
	// but it confirms skills are detected properly before that point.
	err := runBuild(nil, []string{dir})
	if err == nil {
		t.Fatal("expected error (docker not available in test), got nil")
	}
	if !contains(err.Error(), "docker") {
		t.Errorf("error = %q, want it to contain 'docker'", err.Error())
	}
}
