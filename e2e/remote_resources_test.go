//go:build e2e

// Tests for the new RemoteMCPServer kind plus the ResourceRef.Kind
// discriminator on AgentSpec.MCPServers.

package e2e

import (
	"fmt"
	"net/http"
	"testing"
)

// verifyRemoteMCPServerExists checks that the RemoteMCPServer exists in the registry via HTTP GET.
func verifyRemoteMCPServerExists(t *testing.T, regURL, name, version string) {
	t.Helper()
	resp := RegistryGet(t, resourceURL(regURL, "remotemcpservers", name, version))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected RemoteMCPServer %s@%s to exist (HTTP 200) but got %d", name, version, resp.StatusCode)
	}
}

// TestDeclarativeApply_RemoteMCPServer covers apply → get → delete for the
// RemoteMCPServer kind. Verifies the row is created and is reachable under
// the canonical /v0/remotemcpservers/{name}/{version} path.
func TestDeclarativeApply_RemoteMCPServer(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	name := "e2e-test/" + UniqueNameWithPrefix("decl-remote-mcp")
	version := "0.0.1-e2e"

	RunArctl(t, tmpDir, "delete", "remote-mcp", name, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "remote-mcp", name, "--version", version, "--registry-url", regURL)
	})

	yaml := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: RemoteMCPServer
metadata:
  name: %s
  version: "%s"
spec:
  title: E2E Remote MCP Server
  description: Hosted MCP endpoint for the declarative-apply E2E test
  remote:
    type: streamable-http
    url: https://example.test/mcp
`, name, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "remote-mcp.yaml", yaml)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "RemoteMCPServer/"+name)

	verifyRemoteMCPServerExists(t, regURL, name, version)
}

// TestDeclarativeApply_AgentReferencesRemoteMCPServer covers the
// ResourceRef.Kind discriminator: an Agent references a RemoteMCPServer
// from its spec.mcpServers list. Apply must accept the explicit
// Kind=RemoteMCPServer ref (the validator default-falls-back to
// MCPServer when Kind is empty).
func TestDeclarativeApply_AgentReferencesRemoteMCPServer(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	remoteName := "e2e-test/" + UniqueNameWithPrefix("decl-remote-mcp-ref")
	agentName := UniqueAgentName("decl-agent-ref-remote")
	version := "0.0.1-e2e"

	RunArctl(t, tmpDir, "delete", "remote-mcp", remoteName, "--version", version, "--registry-url", regURL)
	RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "remote-mcp", remoteName, "--version", version, "--registry-url", regURL)
	})

	yaml := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: RemoteMCPServer
metadata:
  name: %s
  version: "%s"
spec:
  remote:
    type: streamable-http
    url: https://example.test/mcp
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/agent-ref-remote:latest
  description: Agent that wires in a RemoteMCPServer via Kind discrimination
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
  mcpServers:
    - kind: RemoteMCPServer
      name: %s
      version: "%s"
`, remoteName, version, agentName, version, remoteName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "stack.yaml", yaml)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "RemoteMCPServer/"+remoteName)
	RequireOutputContains(t, result, "Agent/"+agentName)

	verifyRemoteMCPServerExists(t, regURL, remoteName, version)
	verifyAgentExists(t, regURL, agentName, version)
}
