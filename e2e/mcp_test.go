//go:build e2e

// Tests for standalone MCP server CLI commands (init, build) that do not
// require the registry. Registry-dependent MCP tests live in
// mcp_publish_test.go.

package e2e

import (
	"path/filepath"
	"testing"
)

// TestMCPInitPythonAndBuild tests the full local MCP lifecycle for Python:
// init an MCP server and build its Docker image.
func TestMCPInitPythonAndBuild(t *testing.T) {
	tmpDir := t.TempDir()
	mcpName := UniqueNameWithPrefix("e2e-mcp")

	// Step 1: MCP Init (Python)
	t.Run("init", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "init", "python", mcpName,
			"--non-interactive",
			"--no-git",
		)
		RequireSuccess(t, result)

		mcpDir := filepath.Join(tmpDir, mcpName)
		RequireDirExists(t, mcpDir)
		RequireFileExists(t, filepath.Join(mcpDir, "mcp.yaml"))
		RequireFileExists(t, filepath.Join(mcpDir, "Dockerfile"))
		RequireFileExists(t, filepath.Join(mcpDir, "pyproject.toml"))
	})

	// Step 2: MCP Build
	t.Run("build", func(t *testing.T) {
		mcpDir := filepath.Join(tmpDir, mcpName)

		// The default image name for mcp build is <name>:<version> from mcp.yaml
		// Version defaults to 0.1.0
		defaultImage := mcpName + ":0.1.0"
		CleanupDockerImage(t, defaultImage)

		result := RunArctl(t, tmpDir, "mcp", "build", mcpDir)
		RequireSuccess(t, result)

		if !DockerImageExists(t, defaultImage) {
			t.Fatalf("Expected Docker image %s to exist after build", defaultImage)
		}
	})

	// Note: mcp run is not tested here because it starts the agentregistry daemon
	// via Docker Compose, which conflicts with the Kind-based e2e setup. The daemon
	// image is only available in the Kind cluster registry, not the local Docker registry.
}

// TestMCPInitGo tests "mcp init go" with a custom Go module name and verifies
// the generated project structure (go.mod, main.go, mcp.yaml).
func TestMCPInitGo(t *testing.T) {
	tmpDir := t.TempDir()
	mcpName := UniqueNameWithPrefix("e2e-mcp-go")

	result := RunArctl(t, tmpDir,
		"mcp", "init", "go", mcpName,
		"--non-interactive",
		"--no-git",
		"--go-module-name", "github.com/test/"+mcpName,
	)
	RequireSuccess(t, result)

	mcpDir := filepath.Join(tmpDir, mcpName)
	RequireDirExists(t, mcpDir)
	RequireFileExists(t, filepath.Join(mcpDir, "go.mod"))
	RequireFileExists(t, filepath.Join(mcpDir, "cmd", "server", "main.go"))
	RequireFileExists(t, filepath.Join(mcpDir, "mcp.yaml"))

	// Verify go.mod has correct module name
	RequireFileContains(t, filepath.Join(mcpDir, "go.mod"), "github.com/test/"+mcpName)
}

// TestMCPInitValidation verifies that "mcp init" handles edge cases: showing
// help when no project type is given, and failing when Go init is missing
// --go-module-name in non-interactive mode.
func TestMCPInitValidation(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("missing_project_type_shows_help", func(t *testing.T) {
		// mcp init without a subcommand shows help and exits 0 (Cobra parent command behavior)
		result := RunArctl(t, tmpDir, "mcp", "init")
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "Available Commands")
	})

	t.Run("go_missing_module_name", func(t *testing.T) {
		// Go init without --go-module-name should fail in non-interactive mode
		result := RunArctl(t, tmpDir,
			"mcp", "init", "go", "test-mcp",
			"--non-interactive",
			"--no-git",
		)
		RequireFailure(t, result)
	})
}

// TestMCPBuildWithoutInit verifies that "mcp build" fails when pointed at a
// nonexistent directory.
func TestMCPBuildWithoutInit(t *testing.T) {
	tmpDir := t.TempDir()

	result := RunArctl(t, tmpDir, "mcp", "build", filepath.Join(tmpDir, "nonexistent"))
	RequireFailure(t, result)
}
