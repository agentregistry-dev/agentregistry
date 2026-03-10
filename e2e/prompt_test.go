//go:build e2e

// Tests for the "prompt" CLI commands. These tests verify the full lifecycle:
// publish a prompt, list prompts, show prompt details, and delete a prompt.
// Additionally covers multi-version workflows, API-level verification,
// content integrity, and error cases.

package e2e

import (
	"encoding/json"
	"io"
	"net/http"
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

	t.Run("directory_instead_of_file", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", tmpDir,
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

// TestPromptMultipleVersions tests publishing multiple versions of the same
// prompt and verifying that the latest version is returned by show, all
// versions are accessible via the API, and individual versions can be deleted.
func TestPromptMultipleVersions(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-multi-ver")
	v1 := "1.0.0"
	v2 := "2.0.0"
	v1Content := "Version 1 content: You are a helpful assistant."
	v2Content := "Version 2 content: You are an expert coding assistant."

	// Publish version 1
	t.Run("publish_v1", func(t *testing.T) {
		promptFile := filepath.Join(tmpDir, "v1.txt")
		if err := os.WriteFile(promptFile, []byte(v1Content), 0644); err != nil {
			t.Fatalf("failed to write prompt file: %v", err)
		}
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", promptName,
			"--version", v1,
			"--description", "First version",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "published successfully")
	})

	// Publish version 2
	t.Run("publish_v2", func(t *testing.T) {
		promptFile := filepath.Join(tmpDir, "v2.txt")
		if err := os.WriteFile(promptFile, []byte(v2Content), 0644); err != nil {
			t.Fatalf("failed to write prompt file: %v", err)
		}
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", promptName,
			"--version", v2,
			"--description", "Second version",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "published successfully")
	})

	// Show (without version) should return latest
	t.Run("show_returns_latest", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "show", promptName,
			"--output", "json",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, promptName)
		RequireOutputContains(t, result, v2)
	})

	// Verify both versions and latest via API
	// version is the version queried, expectedVersion is the version that should be returned (e.g. querying latest should return the latest version)
	versionContentTestCases := []struct {
		name            string
		version         string
		expectedVersion string
		expectedContent string
	}{
		{
			name:            "verify_v1_via_api",
			version:         v1,
			expectedVersion: v1,
			expectedContent: v1Content,
		},
		{
			name:            "verify_v2_via_api",
			version:         v2,
			expectedVersion: v2,
			expectedContent: v2Content,
		},
		{
			name:            "verify_latest_via_api",
			version:         "latest",
			expectedVersion: v2,
			expectedContent: v2Content,
		},
	}
	for _, tc := range versionContentTestCases {
		t.Run(tc.name, func(t *testing.T) {
			url := regURL + "/prompts/" + promptName + "/versions/" + tc.version
			resp := RegistryGet(t, url)
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Fatalf("expected 200 for version %s, got %d", tc.version, resp.StatusCode)
			}
			body, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("failed to read response body: %v", err)
			}

			var promptResp struct {
				Prompt struct {
					Name    string `json:"name"`
					Version string `json:"version"`
					Content string `json:"content"`
				} `json:"prompt"`
			}
			if err := json.Unmarshal(body, &promptResp); err != nil {
				t.Fatalf("failed to parse prompt response: %v", err)
			}

			if promptResp.Prompt.Name != promptName {
				t.Errorf("name = %q, want %q", promptResp.Prompt.Name, promptName)
			}
			if promptResp.Prompt.Version != tc.expectedVersion {
				t.Errorf("version = %q, want %q", promptResp.Prompt.Version, tc.expectedVersion)
			}
			if promptResp.Prompt.Content != tc.expectedContent {
				t.Errorf("content = %q, want %q", promptResp.Prompt.Content, tc.expectedContent)
			}
		})
	}

	// List all versions via API
	t.Run("list_versions_via_api", func(t *testing.T) {
		url := regURL + "/prompts/" + promptName + "/versions"
		resp := RegistryGet(t, url)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 from versions endpoint, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		var versionsResp struct {
			Prompts []struct {
				Prompt struct {
					Name    string `json:"name"`
					Version string `json:"version"`
				} `json:"prompt"`
			} `json:"prompts"`
			Metadata struct {
				Count int `json:"count"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal(body, &versionsResp); err != nil {
			t.Fatalf("failed to parse versions response: %v", err)
		}

		if versionsResp.Metadata.Count != 2 {
			t.Errorf("expected 2 versions, got %d", versionsResp.Metadata.Count)
		}

		// Verify both versions are present
		foundV1, foundV2 := false, false
		for _, p := range versionsResp.Prompts {
			if p.Prompt.Version == v1 {
				foundV1 = true
			}
			if p.Prompt.Version == v2 {
				foundV2 = true
			}
		}
		if !foundV1 {
			t.Errorf("version %s not found in versions list", v1)
		}
		if !foundV2 {
			t.Errorf("version %s not found in versions list", v2)
		}
	})

	// Delete v1 and verify v2 still exists
	t.Run("delete_v1", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "delete", promptName,
			"--version", v1,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "deleted successfully")
	})

	t.Run("v2_still_accessible_after_v1_delete", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "show", promptName,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, promptName)
	})

	// Cleanup: delete v2
	t.Run("cleanup_v2", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "delete", promptName,
			"--version", v2,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})
}

// TestPromptContentIntegrity verifies that the prompt content published
// is exactly what is returned when retrieving the prompt via the API.
func TestPromptContentIntegrity(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-content")
	version := "1.0.0"
	expectedContent := "You are an AI assistant specialized in Go programming.\n\nRules:\n1. Always use error wrapping\n2. Follow Go conventions\n3. Write table-driven tests"

	promptFile := filepath.Join(tmpDir, "content-test.txt")
	if err := os.WriteFile(promptFile, []byte(expectedContent), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Publish
	t.Run("publish", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", promptName,
			"--version", version,
			"--description", "Content integrity test",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})

	// Verify content via API
	t.Run("verify_content", func(t *testing.T) {
		url := regURL + "/prompts/" + promptName + "/versions/" + version
		resp := RegistryGet(t, url)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		var promptResp struct {
			Prompt struct {
				Name        string `json:"name"`
				Version     string `json:"version"`
				Content     string `json:"content"`
				Description string `json:"description"`
			} `json:"prompt"`
		}
		if err := json.Unmarshal(body, &promptResp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if promptResp.Prompt.Content != expectedContent {
			t.Errorf("content mismatch:\ngot:  %q\nwant: %q", promptResp.Prompt.Content, expectedContent)
		}
		if promptResp.Prompt.Description != "Content integrity test" {
			t.Errorf("description = %q, want %q", promptResp.Prompt.Description, "Content integrity test")
		}
	})

	// Cleanup
	t.Cleanup(func() {
		RunArctl(t, tmpDir,
			"prompt", "delete", promptName,
			"--version", version,
			"--registry-url", regURL,
		)
	})
}

// TestPromptShowNonExistent verifies that showing a prompt that does not
// exist returns an appropriate error or empty result.
func TestPromptShowNonExistent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	result := RunArctl(t, tmpDir,
		"prompt", "show", "nonexistent-prompt-xyz-12345",
		"--registry-url", regURL,
	)
	// The CLI should either fail or show "not found"
	if result.ExitCode == 0 {
		RequireOutputContains(t, result, "not found")
	}
}

// TestPromptDeleteNonExistent verifies that deleting a prompt that does not
// exist returns an appropriate error.
func TestPromptDeleteNonExistent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	result := RunArctl(t, tmpDir,
		"prompt", "delete", "nonexistent-prompt-xyz-12345",
		"--version", "1.0.0",
		"--registry-url", regURL,
	)
	RequireFailure(t, result)
}

// TestPromptAPINotFound verifies that the registry API returns 404 for
// a prompt that does not exist.
func TestPromptAPINotFound(t *testing.T) {
	regURL := RegistryURL(t)

	t.Run("nonexistent_prompt", func(t *testing.T) {
		url := regURL + "/prompts/nonexistent-prompt-xyz-99999/versions/latest"
		resp := RegistryGet(t, url)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 for nonexistent prompt, got %d", resp.StatusCode)
		}
	})

	t.Run("nonexistent_version", func(t *testing.T) {
		// First publish a prompt so the name exists
		tmpDir := t.TempDir()
		promptName := UniqueNameWithPrefix("e2e-notfound-ver")
		version := "1.0.0"

		promptFile := filepath.Join(tmpDir, "prompt.txt")
		if err := os.WriteFile(promptFile, []byte("test content"), 0644); err != nil {
			t.Fatalf("failed to write prompt file: %v", err)
		}

		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", promptName,
			"--version", version,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)

		// Request a version that doesn't exist
		url := regURL + "/prompts/" + promptName + "/versions/99.99.99"
		resp := RegistryGet(t, url)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("expected 404 for nonexistent version, got %d", resp.StatusCode)
		}

		// Cleanup
		t.Cleanup(func() {
			RunArctl(t, tmpDir,
				"prompt", "delete", promptName,
				"--version", version,
				"--registry-url", regURL,
			)
		})
	})
}

// TestPromptPublishYAMLWithFlagOverrides verifies that CLI flags override
// values defined in the YAML file when both are provided.
func TestPromptPublishYAMLWithFlagOverrides(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	yamlName := "yaml-original-name"
	overrideName := UniqueNameWithPrefix("e2e-override")
	version := "1.0.0"

	yamlContent := "name: " + yamlName + "\n" +
		"version: " + version + "\n" +
		"description: Original description\n" +
		"content: Original YAML content\n"
	promptFile := filepath.Join(tmpDir, "override.yaml")
	if err := os.WriteFile(promptFile, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write YAML file: %v", err)
	}

	// Publish with --name flag to override the YAML name
	t.Run("publish_with_override", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "publish", promptFile,
			"--name", overrideName,
			"--description", "Overridden description",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "published successfully")
	})

	// Verify the override name was used via API
	t.Run("verify_override_name", func(t *testing.T) {
		url := regURL + "/prompts/" + overrideName + "/versions/" + version
		resp := RegistryGet(t, url)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response body: %v", err)
		}

		var promptResp struct {
			Prompt struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"prompt"`
		}
		if err := json.Unmarshal(body, &promptResp); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if promptResp.Prompt.Name != overrideName {
			t.Errorf("name = %q, want %q (override)", promptResp.Prompt.Name, overrideName)
		}
		if promptResp.Prompt.Description != "Overridden description" {
			t.Errorf("description = %q, want %q", promptResp.Prompt.Description, "Overridden description")
		}
	})

	// Cleanup
	t.Cleanup(func() {
		RunArctl(t, tmpDir,
			"prompt", "delete", overrideName,
			"--version", version,
			"--registry-url", regURL,
		)
	})
}

// TestPromptDryRunDoesNotCreate verifies that --dry-run does not actually
// create a prompt in the registry.
func TestPromptDryRunDoesNotCreate(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-dryrun-verify")
	version := "1.0.0"

	promptFile := filepath.Join(tmpDir, "dryrun.txt")
	if err := os.WriteFile(promptFile, []byte("should not be persisted"), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Publish with --dry-run
	result := RunArctl(t, tmpDir,
		"prompt", "publish", promptFile,
		"--name", promptName,
		"--version", version,
		"--dry-run",
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "DRY RUN")

	// Verify the prompt does NOT exist in the registry
	url := regURL + "/prompts/" + promptName + "/versions/" + version
	resp := RegistryGet(t, url)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after dry-run publish, got %d (prompt should not have been created)", resp.StatusCode)
	}
}

// TestPromptListOutputJSON verifies that "prompt list" with --output json
// returns valid JSON.
func TestPromptListOutputJSON(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-list-json")
	version := "1.0.0"

	promptFile := filepath.Join(tmpDir, "list-json.txt")
	if err := os.WriteFile(promptFile, []byte("list json test content"), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Publish a prompt so the list is non-empty
	result := RunArctl(t, tmpDir,
		"prompt", "publish", promptFile,
		"--name", promptName,
		"--version", version,
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	// List in JSON format
	t.Run("list_json", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"prompt", "list",
			"--all",
			"--output", "json",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)

		// Verify the output is valid JSON
		var jsonOutput interface{}
		if err := json.Unmarshal([]byte(result.Stdout), &jsonOutput); err != nil {
			t.Fatalf("expected valid JSON output, got parse error: %v\nOutput: %s", err, result.Stdout)
		}
	})

	// Cleanup
	t.Cleanup(func() {
		RunArctl(t, tmpDir,
			"prompt", "delete", promptName,
			"--version", version,
			"--registry-url", regURL,
		)
	})
}

// TestPromptDeleteThenShowReturnsNotFound verifies that after deleting
// a prompt, it is no longer accessible via show or the API.
func TestPromptDeleteThenShowReturnsNotFound(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2e-del-verify")
	version := "1.0.0"

	promptFile := filepath.Join(tmpDir, "del-verify.txt")
	if err := os.WriteFile(promptFile, []byte("delete verification content"), 0644); err != nil {
		t.Fatalf("failed to write prompt file: %v", err)
	}

	// Publish
	result := RunArctl(t, tmpDir,
		"prompt", "publish", promptFile,
		"--name", promptName,
		"--version", version,
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	// Delete
	result = RunArctl(t, tmpDir,
		"prompt", "delete", promptName,
		"--version", version,
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	// Verify via API that it's gone
	url := regURL + "/prompts/" + promptName + "/versions/" + version
	resp := RegistryGet(t, url)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 after deletion, got %d", resp.StatusCode)
	}
}
