package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Test_TranslateRegistry tests the registry translator without Docker.
func Test_TranslateRegistry(t *testing.T) {
	ctx := context.Background()
	regTranslator := registry.NewTranslator()

	var reqs []*registry.MCPServerRunRequest
	for _, srvJson := range []string{
		`{
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
        "name": "io.github.estruyf/vscode-demo-time",
        "description": "Enables AI assistants to interact with Demo Time and helps build presentations and demos.",
        "repository": {
          "url": "https://github.com/estruyf/vscode-demo-time",
          "source": "git"
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
		reqs = append(reqs, parseServerReqUnit(t, srvJson))
	}

	// Test translation without Docker
	for _, req := range reqs {
		mcpServer, err := regTranslator.TranslateMCPServer(ctx, req)
		if err != nil {
			t.Fatalf("TranslateMCPServer failed: %v", err)
		}
		if mcpServer == nil {
			t.Fatal("mcpServer is nil")
		}
		if mcpServer.Name == "" {
			t.Fatal("mcpServer.Name is empty")
		}
	}
}

// Test_TranslateDockerCompose tests the docker-compose translator without Docker.
func Test_TranslateDockerCompose(t *testing.T) {
	ctx := context.Background()
	runtimeDir := t.TempDir()

	composeTranslator := dockercompose.NewAgentGatewayTranslator(runtimeDir, 18080)
	regTranslator := registry.NewTranslator()

	var reqs []*registry.MCPServerRunRequest
	for _, srvJson := range []string{
		`{
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
        "name": "io.github.estruyf/vscode-demo-time",
        "description": "Enables AI assistants to interact with Demo Time and helps build presentations and demos.",
        "repository": {
          "url": "https://github.com/estruyf/vscode-demo-time",
          "source": "git"
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
		reqs = append(reqs, parseServerReqUnit(t, srvJson))
	}

	// Build desired state
	desiredState := &api.DesiredState{}
	for _, req := range reqs {
		mcpServer, err := regTranslator.TranslateMCPServer(ctx, req)
		if err != nil {
			t.Fatalf("translate mcp server: %v", err)
		}
		desiredState.MCPServers = append(desiredState.MCPServers, mcpServer)
	}

	// Test docker-compose translation
	runtimeCfg, err := composeTranslator.TranslateRuntimeConfig(ctx, desiredState)
	if err != nil {
		t.Fatalf("TranslateRuntimeConfig failed: %v", err)
	}

	if runtimeCfg == nil {
		t.Fatal("runtimeCfg is nil")
	}
	if runtimeCfg.Local.DockerCompose == nil {
		t.Fatal("DockerCompose is nil")
	}
	if runtimeCfg.Local.AgentGateway == nil {
		t.Fatal("AgentGateway is nil")
	}

	// Verify YAML can be generated
	dockerComposeYaml, err := runtimeCfg.Local.DockerCompose.MarshalYAML()
	if err != nil {
		t.Fatalf("failed to marshal docker compose yaml: %v", err)
	}
	if len(dockerComposeYaml) == 0 {
		t.Fatal("docker-compose yaml is empty")
	}
}

func parseServerReqUnit(
	t *testing.T,
	s string,
) *registry.MCPServerRunRequest {
	var server apiv0.ServerJSON
	if err := json.Unmarshal([]byte(s), &server); err != nil {
		t.Fatalf("unmarshal server json: %v", err)
	}
	return &registry.MCPServerRunRequest{RegistryServer: &server}
}

func TestCreateResolvedMCPServerConfigs_UsesDeploymentScopedNames(t *testing.T) {
	reqWithDeployment := parseServerReqUnit(t, `{
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
        "name": "io.github.estruyf/vscode-demo-time",
        "description": "Demo",
        "version": "0.0.55",
        "packages": [{
          "registryType": "npm",
          "registryBaseUrl": "https://registry.npmjs.org",
          "identifier": "@demotime/mcp",
          "version": "0.0.55",
          "transport": {"type": "stdio"}
        }]
      }`)
	reqWithDeployment.DeploymentID = "dep-123"

	reqWithoutDeployment := parseServerReqUnit(t, `{
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
        "name": "io.github.estruyf/vscode-demo-time",
        "description": "Demo",
        "version": "0.0.55",
        "packages": [{
          "registryType": "npm",
          "registryBaseUrl": "https://registry.npmjs.org",
          "identifier": "@demotime/mcp",
          "version": "0.0.55",
          "transport": {"type": "stdio"}
        }]
      }`)

	configs := createResolvedMCPServerConfigs([]*registry.MCPServerRunRequest{reqWithDeployment, reqWithoutDeployment})
	if len(configs) != 2 {
		t.Fatalf("expected 2 resolved configs, got %d", len(configs))
	}
	if configs[0].Name != "io-github-estruyf-vscode-demo-time-dep-123" {
		t.Fatalf("expected deployment-scoped name for first config, got %q", configs[0].Name)
	}
	if configs[1].Name != "io-github-estruyf-vscode-demo-time" {
		t.Fatalf("expected non-scoped name for second config, got %q", configs[1].Name)
	}
}

func TestCreateResolvedMCPServerConfigs_UsesUserProvidedName(t *testing.T) {
	req := parseServerReqUnit(t, `{
        "$schema": "https://static.modelcontextprotocol.io/schemas/2025-09-29/server.schema.json",
        "name": "user/my-mcp-server",
        "description": "Demo",
        "version": "1.0.0",
        "packages": [{
          "registryType": "npm",
          "identifier": "@example/mcp",
          "version": "1.0.0",
          "transport": {"type": "stdio"}
        }]
      }`)
	// User provides a short name in agent.yaml
	req.Name = "my-mcp-server"

	configs := createResolvedMCPServerConfigs([]*registry.MCPServerRunRequest{req})
	if len(configs) != 1 {
		t.Fatalf("expected 1 resolved config, got %d", len(configs))
	}
	// Should use user-provided name, not the registry name
	if configs[0].Name != "my-mcp-server" {
		t.Fatalf("expected user-provided name 'my-mcp-server', got %q", configs[0].Name)
	}
}
