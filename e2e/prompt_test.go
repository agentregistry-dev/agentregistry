//go:build e2e

// Tests for the "prompt" CLI commands. These tests verify the full lifecycle:
// publish a prompt, list prompts, show prompt details, and delete a prompt.

package e2e

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPromptPublishListShowDelete tests the full prompt lifecycle via the CLI:
// publish a text file prompt, list prompts to verify it appears, show its
// details, and finally delete it.
func TestPromptPublishListShowDelete(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-prompt")
	version := "0.0.1-e2e"

	// Create a text prompt file
	promptFile := filepath.Join(tmpDir, "system-prompt.txt")
	if err := os.WriteFile(promptFile, []byte("You are a helpful coding assistant."), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Step 1: Publish the prompt
	t.Run("publish_text", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", promptName,
			"--version", version,
			"--description", "E2E test prompt",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "published successfully")
	})

	// Step 2: List prompts and verify the published one appears
	t.Run("list", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "list",
			"--all",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, promptName)
	})

	// Step 3: Show prompt details
	t.Run("show", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "show", promptName,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, promptName)
	})

	// Step 4: Show in JSON format
	t.Run("show_json", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "show", promptName,
			"--output", "json",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, promptName)
	})

	// Step 5: Delete the prompt
	t.Run("delete", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "delete", promptName,
			"--version", version,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "deleted successfully")
	})
}

// TestPromptPublishYAML tests publishing a prompt from a YAML file.
func TestPromptPublishYAML(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-yaml-prompt")
	version := "1.0.0"

	// Create a YAML prompt file
	yamlContent := "name: " + promptName + "\n" +
		"version: " + version + "\n" +
		"description: E2E YAML prompt\n" +
		"content: You are a code review assistant.\n"
	promptFile := filepath.Join(tmpDir, "prompt.yaml")
	if err := os.WriteFile(promptFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write YAML file: %v", err)
	}

	// Publish the YAML prompt
	t.Run("publish_yaml", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "published successfully")
	})

	// Verify it appears in the list
	t.Run("verify_in_list", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "list",
			"--all",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, promptName)
	})

	// Cleanup
	t.Run("cleanup", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "delete", promptName,
			"--version", version,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})
}

// TestPromptPublishDryRun tests the --dry-run flag does not create a prompt.
func TestPromptPublishDryRun(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-dry-prompt")

	promptFile := filepath.Join(tmpDir, "dry.txt")
	if err := os.WriteFile(promptFile, []byte("dry run content"), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Publish with --dry-run
	t.Run("dry_run", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", promptName,
			"--version", "1.0.0",
			"--dry-run",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "DRY RUN")
	})
}

// TestPromptPublishValidation verifies that "prompt publish" rejects
// requests with missing required fields.
func TestPromptPublishValidation(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	promptFile := filepath.Join(tmpDir, "prompt.txt")
	if err := os.WriteFile(promptFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	t.Run("missing_name", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--version", "1.0.0",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
	})

	t.Run("missing_version", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", "missing-version-prompt",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
	})

	t.Run("nonexistent_file", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", "/nonexistent/file.txt",
			"--name", "test",
			"--version", "1.0.0",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
	})
}

// TestPromptDeleteValidation verifies that "prompt delete" requires
// the --version flag.
func TestPromptDeleteValidation(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("missing_version_flag", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "delete", "some-prompt",
		)
		RequireFailure(t, result)
	})
}
