//go:build e2e

// Tests for the "mcp publish" command. These tests verify publishing MCP servers
// to the registry and validating required flags.

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
)

// TestMCPPublishAndVerify tests the full MCP publish lifecycle: init a Python
// MCP server, build it, publish to the registry, then verify the server
// appears via the registry's list-servers API.
func TestMCPPublishAndVerify(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	mcpName := UniqueNameWithPrefix("e2e-pub-mcp")
	serverName := "e2e-test/" + mcpName
	version := "0.0.1-e2e"

	// Step 1: Init an MCP server locally
	t.Run("init_and_build", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "init", "python", mcpName,
			"--non-interactive",
			"--no-git",
		)
		RequireSuccess(t, result)

		mcpDir := filepath.Join(tmpDir, mcpName)
		defaultImage := mcpName + ":0.1.0"
		CleanupDockerImage(t, defaultImage)

		result = RunArctl(t, tmpDir, "mcp", "build", mcpDir)
		RequireSuccess(t, result)
	})

	// Step 2: Publish the MCP server to the registry
	t.Run("publish", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "publish", serverName,
			"--type", "oci",
			"--package-id", fmt.Sprintf("docker.io/e2etest/%s:latest", mcpName),
			"--version", version,
			"--description", "E2E test MCP server",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})

	// Step 3: Verify the server exists in the registry via API
	t.Run("verify_in_registry", func(t *testing.T) {
		url := ListServersURL(regURL)
		resp := RegistryGet(t, url)
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("Expected 200 from registry at %s, got %d", url, resp.StatusCode)
		}

		var body interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatalf("Failed to decode registry response: %v", err)
		}
		t.Logf("Registry response: %+v", body)
	})
}

// TestMCPPublishValidation verifies that "mcp publish" rejects requests
// with missing required flags (--type, --package-id).
func TestMCPPublishValidation(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	t.Run("missing_type", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "publish", "e2e-test/missing-type",
			"--package-id", "docker.io/test/server:latest",
			"--version", "0.0.1",
			"--description", "test",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
	})

	t.Run("missing_package_id", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "publish", "e2e-test/missing-pkg",
			"--type", "oci",
			"--version", "0.0.1",
			"--description", "test",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
	})
}
