//go:build e2e

// Tests for remote MCP servers (MCPServer with Spec.Remote set) and how
// Agents reference them via spec.mcpServers.

package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// verifyMCPServerExists checks that the MCPServer exists in the registry via HTTP GET.
func verifyMCPServerExists(t *testing.T, regURL, name, tag string) {
	t.Helper()
	resp := RegistryGet(t, resourceURL(regURL, "mcpservers", name, tag))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected MCPServer %s@%s to exist (HTTP 200) but got %d", name, tag, resp.StatusCode)
	}
}

// TestDeclarativeApply_RemoteMCPServer covers apply → get → delete for a
// remote MCPServer (Spec.Remote set). Verifies the row is created and is
// reachable under the canonical /v0/mcpservers/{name}/{tag} path.
func TestDeclarativeApply_RemoteMCPServer(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	name := "e2e-test/" + UniqueNameWithPrefix("decl-remote-mcp")
	tag := "latest"

	RunArctl(t, tmpDir, "delete", "mcpserver", name, "--tag", tag, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcpserver", name, "--tag", tag, "--registry-url", regURL)
	})

	yaml := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
spec:
  title: E2E Remote MCP Server
  description: Hosted MCP endpoint for the declarative-apply E2E test
  remote:
    type: streamable-http
    url: https://example.test/mcp
`, name)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "remote-mcp.yaml", yaml)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "MCPServer/"+name)

	verifyMCPServerExists(t, regURL, name, tag)
}

// TestDeclarativeApply_AgentReferencesRemoteMCPServer covers an Agent
// that references a remote MCPServer from its spec.mcpServers list.
// Apply must accept the ref with Kind=MCPServer (or empty, which
// defaults to MCPServer); the server distinguishes bundled vs remote by
// whether Spec.Source or Spec.Remote is set.
func TestDeclarativeApply_AgentReferencesRemoteMCPServer(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	remoteName := "e2e-test/" + UniqueNameWithPrefix("decl-remote-mcp-ref")
	agentName := UniqueAgentName("decl-agent-ref-remote")
	tag := "latest"

	RunArctl(t, tmpDir, "delete", "mcpserver", remoteName, "--tag", tag, "--registry-url", regURL)
	RunArctl(t, tmpDir, "delete", "agent", agentName, "--tag", tag, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--tag", tag, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "mcpserver", remoteName, "--tag", tag, "--registry-url", regURL)
	})

	yaml := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
spec:
  remote:
    type: streamable-http
    url: https://example.test/mcp
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
spec:
  image: ghcr.io/e2e-test/agent-ref-remote:latest
  description: Agent that wires in a remote MCPServer
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
  mcpServers:
    - kind: MCPServer
      name: %s
      tag: %s
`, remoteName, agentName, remoteName, tag)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "stack.yaml", yaml)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "MCPServer/"+remoteName)
	RequireOutputContains(t, result, "Agent/"+agentName)

	verifyMCPServerExists(t, regURL, remoteName, tag)
	verifyAgentExists(t, regURL, agentName, tag)
}
