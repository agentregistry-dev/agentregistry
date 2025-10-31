//go:build integration

package runtime

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Test_AgentRegistryRuntime_ReconcileMCPServers_DockerIntegration tests that
// ReconcileMCPServers actually starts Docker containers correctly.
// This test requires Docker to be available.
func Test_AgentRegistryRuntime_ReconcileMCPServers_DockerIntegration(t *testing.T) {
	ctx := context.Background()
	// Create a temp runtime dir
	runtimeDir := t.TempDir()

	// Real docker-compose translator, using the temp runtime dir for working files
	// Note: We pass executeMCPRuntime=true to actually start Docker containers
	composeTranslator := dockercompose.NewAgentGatewayTranslator(runtimeDir, 18080)
	regTranslator := registry.NewTranslator()

	r := NewAgentRegistryRuntime(regTranslator, composeTranslator, runtimeDir, true)

	var reqs []*registry.MCPServerRunRequest
	for _, srvJson := range []string{
		`{
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
        "name": "io.github.estruyf/vscode-demo-time",
        "description": "Enables AI assistants to interact with Demo Time and helps build presentations and demos.",
        "repository": {
          "url": "https://github.com/estruyf/vscode-demo-time",
          "source": "github"
        },
        "version": "0.0.55",
        "packages": [
          {
            "registryType": "npm",
            "registryBaseUrl": "https://registry.npmjs.org",
            "identifier": "@demotime/mcp",
            "version": "0.0.55",
            "transport": {
              "type": "stdio"
            }
          }
        ]
      }`,
	} {
		reqs = append(reqs, parseServerReq(t, srvJson))
	}

	if err := r.ReconcileMCPServers(ctx, reqs); err != nil {
		t.Fatalf("ReconcileMCPServers: %v", err)
	}

	// Give docker a brief moment to start containers
	time.Sleep(2 * time.Second)

	// Verify docker compose ps works in the runtime dir
	{
		cmd := exec.Command("docker", "compose", "ps")
		cmd.Dir = runtimeDir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("docker compose ps failed: %v, output: %s", err, string(out))
		}
		if !strings.Contains(string(out), "agent_gateway") {
			t.Fatalf("expected agent_gateway service in compose ps, got: %s", string(out))
		}
	}

	// Cleanup: bring the stack down
	{
		cmd := exec.Command("docker", "compose", "down", "-v")
		cmd.Dir = runtimeDir
		_ = cmd.Run()
	}
}

func parseServerReq(
	t *testing.T,
	s string,
) *registry.MCPServerRunRequest {
	var server apiv0.ServerJSON
	if err := json.Unmarshal([]byte(s), &server); err != nil {
		t.Fatalf("unmarshal server json: %v", err)
	}
	return &registry.MCPServerRunRequest{RegistryServer: &server}
}
