//go:build e2e

// Tests for the "skill publish" command. These tests verify publishing skills
// to the registry via both --git and --docker-image flags.

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// --- skill build tests ---

// TestSkillBuild tests that "arctl skill build" builds a Docker image from a
// skill folder containing SKILL.md.
func TestSkillBuild(t *testing.T) {
	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2e-build-skill")
	imageName := "localhost/e2e-test/" + skillName + ":latest"

	// Create skill folder with SKILL.md
	skillDir := filepath.Join(tmpDir, skillName)
	createSkillDir(t, skillDir, skillName, "E2E build test skill")

	CleanupDockerImage(t, imageName)

	t.Run("build_succeeds", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "build", skillDir,
			"--image", imageName,
		)
		RequireSuccess(t, result)
	})

	t.Run("image_exists", func(t *testing.T) {
		if !DockerImageExists(t, imageName) {
			t.Fatalf("expected Docker image %s to exist after build", imageName)
		}
	})
}

// NOTE: Basic build error cases (missing --image, invalid dir, no SKILL.md)
// are covered by unit tests in internal/cli/skill/build_test.go.

// TestSkillBuildWithPlatform tests that "arctl skill build" accepts the
// --platform flag and produces a valid image.
func TestSkillBuildWithPlatform(t *testing.T) {
	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2e-plat-skill")
	imageName := "localhost/e2e-test/" + skillName + ":latest"

	skillDir := filepath.Join(tmpDir, skillName)
	createSkillDir(t, skillDir, skillName, "E2E platform test skill")

	CleanupDockerImage(t, imageName)

	result := RunArctl(t, tmpDir,
		"skill", "build", skillDir,
		"--image", imageName,
		"--platform", "linux/amd64",
	)
	RequireSuccess(t, result)
}

// --- skill publish tests ---

// TestSkillPublishGitHub tests publishing a skill with --git flag and
// verifying it appears in the registry with the correct repository metadata.
func TestSkillPublishGitHub(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2e-gh-skill")
	version := "0.0.1-e2e"
	githubRepo := "https://github.com/agentregistry-dev/skills/tree/main/artifacts-builder"

	// Create a skill folder with SKILL.md
	skillDir := filepath.Join(tmpDir, skillName)
	createSkillDir(t, skillDir, skillName, "E2E test skill from GitHub")

	// Step 1: Publish with --git
	t.Run("publish", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--git", githubRepo,
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
		skillResp := fetchSkillFromAPI(t, regURL, skillName, version)

		if skillResp.Name != skillName {
			t.Errorf("name = %q, want %q", skillResp.Name, skillName)
		}
		if skillResp.Repository == nil {
			t.Fatal("expected repository to be set, got nil")
		}
		if skillResp.Repository.URL != githubRepo {
			t.Errorf("repository.url = %q, want %q", skillResp.Repository.URL, githubRepo)
		}
		if skillResp.Repository.Source != "git" {
			t.Errorf("repository.source = %q, want %q", skillResp.Repository.Source, "git")
		}
		if len(skillResp.Packages) != 0 {
			t.Errorf("expected no packages for GitHub-published skill, got %d", len(skillResp.Packages))
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

// TestSkillPublishDockerImageDirect tests publishing a skill with --docker-image
// flag in direct mode (no local SKILL.md) and verifying the package metadata.
func TestSkillPublishDockerImageDirect(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2e-docker-skill")
	version := "0.0.1-e2e"
	dockerImage := "docker.io/test/" + skillName + ":v0.0.1"

	// Publish with --docker-image (direct mode, no local folder)
	t.Run("publish", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillName,
			"--docker-image", dockerImage,
			"--version", version,
			"--description", "E2E Docker skill",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})

	// Verify the skill exists and has Docker package metadata
	t.Run("verify_package_metadata", func(t *testing.T) {
		skillResp := fetchSkillFromAPI(t, regURL, skillName, version)

		if skillResp.Name != skillName {
			t.Errorf("name = %q, want %q", skillResp.Name, skillName)
		}
		if skillResp.Repository != nil {
			t.Errorf("expected repository to be nil for Docker publish, got %+v", skillResp.Repository)
		}
		if len(skillResp.Packages) != 1 {
			t.Fatalf("expected 1 package, got %d", len(skillResp.Packages))
		}
		pkg := skillResp.Packages[0]
		if pkg.RegistryType != "docker" {
			t.Errorf("package registryType = %q, want %q", pkg.RegistryType, "docker")
		}
		if pkg.Identifier != dockerImage {
			t.Errorf("package identifier = %q, want %q", pkg.Identifier, dockerImage)
		}
	})

	t.Cleanup(func() {
		RunArctl(t, tmpDir,
			"skill", "delete", skillName,
			"--version", version,
			"--registry-url", regURL,
		)
	})
}

// TestSkillPublishDockerImageFromFolder tests publishing a skill with
// --docker-image from a local folder containing SKILL.md.
func TestSkillPublishDockerImageFromFolder(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2e-folder-docker")
	version := "0.0.1-e2e"
	dockerImage := "docker.io/test/" + skillName + ":v0.0.1"

	// Create a skill folder with SKILL.md
	skillDir := filepath.Join(tmpDir, skillName)
	createSkillDir(t, skillDir, skillName, "E2E folder Docker skill")

	// Publish with --docker-image from folder
	t.Run("publish", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--docker-image", dockerImage,
			"--version", version,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})

	// Verify the skill has correct metadata from SKILL.md and Docker package
	t.Run("verify_metadata", func(t *testing.T) {
		skillResp := fetchSkillFromAPI(t, regURL, skillName, version)

		if skillResp.Name != skillName {
			t.Errorf("name = %q, want %q", skillResp.Name, skillName)
		}
		if skillResp.Repository != nil {
			t.Errorf("expected repository to be nil for Docker publish, got %+v", skillResp.Repository)
		}
		if len(skillResp.Packages) != 1 {
			t.Fatalf("expected 1 package, got %d", len(skillResp.Packages))
		}
		pkg := skillResp.Packages[0]
		if pkg.RegistryType != "docker" {
			t.Errorf("package registryType = %q, want %q", pkg.RegistryType, "docker")
		}
		if pkg.Identifier != dockerImage {
			t.Errorf("package identifier = %q, want %q", pkg.Identifier, dockerImage)
		}
	})

	t.Cleanup(func() {
		RunArctl(t, tmpDir,
			"skill", "delete", skillName,
			"--version", version,
			"--registry-url", regURL,
		)
	})
}

// TestSkillBuildAndPublish tests the full workflow: build a skill image,
// then publish it referencing the built image.
func TestSkillBuildAndPublish(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2e-full-flow")
	version := "0.0.1-e2e"
	imageName := "localhost/e2e-test/" + skillName + ":v0.0.1"

	// Create skill folder
	skillDir := filepath.Join(tmpDir, skillName)
	createSkillDir(t, skillDir, skillName, "E2E build+publish skill")

	CleanupDockerImage(t, imageName)

	// Step 1: Build
	t.Run("build", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "build", skillDir,
			"--image", imageName,
		)
		RequireSuccess(t, result)
	})

	// Step 2: Publish referencing the built image
	t.Run("publish", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--docker-image", imageName,
			"--version", version,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})

	// Step 3: Verify
	t.Run("verify", func(t *testing.T) {
		skillResp := fetchSkillFromAPI(t, regURL, skillName, version)

		if skillResp.Name != skillName {
			t.Errorf("name = %q, want %q", skillResp.Name, skillName)
		}
		if len(skillResp.Packages) != 1 {
			t.Fatalf("expected 1 package, got %d", len(skillResp.Packages))
		}
		if skillResp.Packages[0].Identifier != imageName {
			t.Errorf("package identifier = %q, want %q", skillResp.Packages[0].Identifier, imageName)
		}
	})

	t.Cleanup(func() {
		RunArctl(t, tmpDir,
			"skill", "delete", skillName,
			"--version", version,
			"--registry-url", regURL,
		)
	})
}

// --- validation tests ---

// TestSkillPublishValidation verifies that "skill publish" rejects requests
// when neither --docker-image nor --git is provided.
func TestSkillPublishValidation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a minimal skill folder
	skillDir := filepath.Join(tmpDir, "test-skill")
	createSkillDir(t, skillDir, "test", "test")

	t.Run("missing_both_flags", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "skill", "publish", skillDir)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "at least one of the flags")
	})

	t.Run("mutually_exclusive_flags", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--docker-url", "docker.io/test",
			"--git", "https://github.com/test/repo",
		)
		RequireFailure(t, result)
	})

	t.Run("missing_version_with_docker_image", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--docker-image", "docker.io/test/test:latest",
			"--registry-url", "http://localhost:12121/v0",
		)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "--version is required")
	})

	t.Run("missing_version_with_github", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"skill", "publish", skillDir,
			"--git", "https://github.com/test/repo",
			"--registry-url", "http://localhost:12121/v0",
		)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "--version is required")
	})

	t.Run("directory_without_skill_md", func(t *testing.T) {
		emptyDir := filepath.Join(tmpDir, "no-skill")
		os.MkdirAll(emptyDir, 0755)

		result := RunArctl(t, tmpDir,
			"skill", "publish", emptyDir,
			"--git", "https://github.com/test/repo",
			"--version", "1.0.0",
			"--registry-url", "http://localhost:12121/v0",
		)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "no valid skills found at path")
	})
}

// --- dry-run tests ---

// TestSkillPublishDryRunGitHub verifies that --dry-run with --git shows
// the intended action without actually publishing.
func TestSkillPublishDryRunGitHub(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "dry-run-skill")
	createSkillDir(t, skillDir, "dry-run-test", "test")

	result := RunArctl(t, tmpDir,
		"skill", "publish", skillDir,
		"--git", "https://github.com/agentregistry-dev/skills/tree/main/artifacts-builder",
		"--version", "1.0.0",
		"--dry-run",
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "DRY RUN")
	RequireOutputContains(t, result, "dry-run-test")
}

// TestSkillPublishDryRunDockerImage verifies that --dry-run with --docker-image
// shows the intended action without actually publishing.
func TestSkillPublishDryRunDockerImage(t *testing.T) {
	tmpDir := t.TempDir()

	skillDir := filepath.Join(tmpDir, "dry-run-docker")
	createSkillDir(t, skillDir, "dry-run-docker-test", "test")

	result := RunArctl(t, tmpDir,
		"skill", "publish", skillDir,
		"--docker-image", "docker.io/test/dry-run:v1.0.0",
		"--version", "1.0.0",
		"--dry-run",
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "DRY RUN")
	RequireOutputContains(t, result, "dry-run-docker-test")
}

// TestSkillPublishDirectDryRun verifies that direct registration mode
// works with --dry-run (no local SKILL.md needed).
func TestSkillPublishDirectDryRun(t *testing.T) {
	tmpDir := t.TempDir()

	result := RunArctl(t, tmpDir,
		"skill", "publish", "direct-test-skill",
		"--git", "https://github.com/agentregistry-dev/skills/tree/main/artifacts-builder",
		"--version", "1.0.0",
		"--description", "A remotely hosted skill",
		"--dry-run",
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "DRY RUN")
	RequireOutputContains(t, result, "direct-test-skill")
}

// --- helpers ---

func createSkillDir(t *testing.T, dir, name, description string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create skill dir: %v", err)
	}
	skillMd := "---\nname: " + name + "\ndescription: " + description + "\n---\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skillMd), 0644); err != nil {
		t.Fatalf("failed to write SKILL.md: %v", err)
	}
}

type skillAPIResponse struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	Repository *struct {
		URL    string `json:"url"`
		Source string `json:"source"`
	} `json:"repository"`
	Packages []struct {
		RegistryType string `json:"registryType"`
		Identifier   string `json:"identifier"`
		Version      string `json:"version"`
	} `json:"packages"`
}

func fetchSkillFromAPI(t *testing.T, regURL, skillName, version string) skillAPIResponse {
	t.Helper()
	url := regURL + "/skills/" + skillName + "/versions/" + version
	resp := RegistryGet(t, url)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from skill endpoint %s, got %d", url, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var wrapper struct {
		Skill skillAPIResponse `json:"skill"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		t.Fatalf("failed to parse skill response: %v\nbody: %s", err, string(body))
	}

	return wrapper.Skill
}
