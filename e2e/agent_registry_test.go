//go:build e2e

// Tests for agent CLI commands that interact with the registry (add-mcp,
// publish).

package e2e

import (
	"fmt"
	"path/filepath"
	"testing"
)

// TestAgentAddMCPAndBuild tests adding a registry-published MCP server to an
// agent. It publishes a test MCP server, inits an agent, adds the MCP via
// "agent add-mcp", verifies agent.yaml is updated, and builds the result.
func TestAgentAddMCPAndBuild(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	mcpName := UniqueNameWithPrefix("e2e-addmcp-srv")
	agentName := UniqueAgentName("e2eaddmcpagent")
	serverName := "e2e-test/" + mcpName
	version := "0.0.1-e2e"

	// Step 1: Publish a test MCP server to the registry
	t.Run("publish_mcp", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "publish", serverName,
			"--type", "oci",
			"--package-id", fmt.Sprintf("docker.io/e2etest/%s:latest", mcpName),
			"--version", version,
			"--description", "E2E test MCP for add-mcp test",
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})

	// Step 2: Init a test agent
	t.Run("init_agent", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"agent", "init", "adk", "python",
			"--model-name", "gemini-2.5-flash",
			agentName,
		)
		RequireSuccess(t, result)
		RequireDirExists(t, filepath.Join(tmpDir, agentName))
	})

	// Step 3: Add the MCP server from the registry
	t.Run("add_mcp", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"agent", "add-mcp", mcpName,
			"--registry-url", regURL,
			"--registry-server-name", serverName,
			"--registry-server-version", version,
			"--env", "MCP_TRANSPORT_MODE=http",
			"--env", "HOST=0.0.0.0",
			"--project-dir", filepath.Join(tmpDir, agentName),
		)
		RequireSuccess(t, result)

		agentYaml := filepath.Join(tmpDir, agentName, "agent.yaml")
		RequireFileContains(t, agentYaml, mcpName)
	})

	// Step 4: Build the agent with the MCP server
	t.Run("build_with_mcp", func(t *testing.T) {
		agentDir := filepath.Join(tmpDir, agentName)

		result := RunArctl(t, tmpDir, "agent", "build", agentDir)
		RequireSuccess(t, result)
	})
}

// TestAgentPublish tests publishing an agent to the registry from a local directory.
func TestAgentPublish(t *testing.T) {
	regURL := RegistryURL(t)

	tmpDir := t.TempDir()
	agentName := UniqueAgentName("e2epubagent")

	// Step 1: Init the agent
	t.Run("init", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"agent", "init", "adk", "python",
			"--model-name", "gemini-2.5-flash",
			agentName,
			"--description", "E2E test agent for publish",
		)
		RequireSuccess(t, result)
	})

	// Step 2: Publish the agent from local directory
	t.Run("publish", func(t *testing.T) {
		agentDir := filepath.Join(tmpDir, agentName)
		result := RunArctl(t, tmpDir,
			"agent", "publish", agentDir,
			"--registry-url", regURL,
		)
		RequireSuccess(t, result)
	})
}
