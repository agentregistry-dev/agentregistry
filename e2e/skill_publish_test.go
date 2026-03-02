//go:build e2e

// Tests for the "skill publish" command. These tests verify publishing skills
// to the registry via both --github and --docker-url flags.

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestSkillPublishGitHub tests publishing a skill with --github flag and
// verifying it appears in the registry with the correct repository metadata.
func TestSkillPublishGitHub(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2e-gh-skill")
	version := "0.0.1-e2e"
	githubRepo := "https://github.com/agentregistry-dev/skills/tree/main/artifacts-builder"

	// Create a skill folder with SKILL.md
	skillDir := filepath.Join(tmpDir, skillName)
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillMd := "---\nname: " + skillName + "\ndescription: E2E test skill from GitHub\n---\n# Test Skill\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(skillMd), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	// Step 1: Publish with --github
	t.Run("publish", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--github", githubRepo,
			"--version", version,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})

	// Step 2: Verify the skill exists in the registry via CLI
	t.Run("verify_via_show", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "show", skillName,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, skillName)
	})

	// Step 3: Verify repository metadata via API
	t.Run("verify_repository_metadata", func(t *testing.T) {
		url := regURL + "/skills/" + skillName + "/versions/" + version
		resp := RegistryGet(t, url)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from skill endpoint, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		var skillResp struct {
			Skill struct {
				Name       string `json:"name"`
				Version    string `json:"version"`
				Repository *struct {
					URL    string `json:"url"`
					Source string `json:"source"`
				} `json:"repository"`
				Packages []interface{} `json:"packages"`
			} `json:"skill"`
		}
		if err := json.Unmarshal(body, &skillResp); err != nil {
			t.Fatalf("failed to parse skill response: %v", err)
		}

		if skillResp.Skill.Name != skillName {
			t.Errorf("name = %q, want %q", skillResp.Skill.Name, skillName)
		}
		if skillResp.Skill.Repository == nil {
			t.Fatal("expected repository to be set, got nil")
		}
		if skillResp.Skill.Repository.URL != githubRepo {
			t.Errorf("repository.url = %q, want %q", skillResp.Skill.Repository.URL, githubRepo)
		}
		if skillResp.Skill.Repository.Source != "github" {
			t.Errorf("repository.source = %q, want %q", skillResp.Skill.Repository.Source, "github")
		}
		if len(skillResp.Skill.Packages) != 0 {
			t.Errorf("expected no packages for GitHub-published skill, got %d", len(skillResp.Skill.Packages))
		}
	})

	// Cleanup: delete the skill from the registry
	t.Cleanup(func() {
		RunArctl(t, tmpDir,
			"skill", "delete", skillName,
			"--version", version,
			"--registry-url", regURL,
		)
	})
}

// TestSkillPublishValidation verifies that "skill publish" rejects requests
// when neither --docker-url nor --github is provided.
func TestSkillPublishValidation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a minimal skill folder
	skillDir := filepath.Join(tmpDir, "test-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: test\n---\n"), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	t.Run("missing_both_flags", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "skill", "publish", skillDir)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "at least one of the flags")
	})

	t.Run("mutually_exclusive_flags", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--docker-url", "docker.io/test",
			"--github", "https://github.com/test/repo",
		)
		RequireFailure(t, result)
	})
}

// TestSkillPublishDryRunGitHub verifies that --dry-run with --github shows
// the intended action without actually publishing.
func TestSkillPublishDryRunGitHub(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "dry-run-skill")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: dry-run-test\ndescription: test\n---\n"), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}

	result := RunArctl(t, tmpDir,
		"skill", "publish", skillDir,
		"--github", "https://github.com/agentregistry-dev/skills/tree/main/artifacts-builder",
		"--version", "1.0.0",
		"--dry-run",
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "DRY RUN")
	RequireOutputContains(t, result, "dry-run-test")
}

// TestSkillPublishDirectDryRun verifies that direct registration mode
// works with --dry-run (no local SKILL.md needed).
func TestSkillPublishDirectDryRun(t *testing.T) {
	tmpDir := t.TempDir()

	result := RunArctl(t, tmpDir,
		"skill", "publish", "direct-test-skill",
		"--github", "https://github.com/agentregistry-dev/skills/tree/main/artifacts-builder",
		"--version", "1.0.0",
		"--description", "A remotely hosted skill",
		"--dry-run",
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "DRY RUN")
	RequireOutputContains(t, result, "direct-test-skill")
}
