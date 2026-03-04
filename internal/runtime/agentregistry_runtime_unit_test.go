package runtime

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	"github.com/agentregistry-dev/agentregistry/pkg/models"

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
func TestBuildDesiredState_IncludesResolvedMCPServersForAgent(t *testing.T) {
	r := &agentRegistryRuntime{
		registryTranslator: registry.NewTranslator(),
		runtimeDir:         t.TempDir(),
		verbose:            false,
	}

	resolvedReq := parseServerReqUnit(t, `{
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
	resolvedReq.DeploymentID = "dep-agent-123"

	agentReq := &registry.AgentRunRequest{
		RegistryAgent: &models.AgentJSON{
			AgentManifest: models.AgentManifest{
				Name:          "planner-agent",
				Image:         "ghcr.io/example/planner:1.0.0",
				ModelProvider: "anthropic",
				ModelName:     "claude-sonnet-4-5",
			},
			Version: "1.0.0",
		},
		DeploymentID:       "dep-agent-123",
		EnvValues:          map[string]string{"KAGENT_NAMESPACE": "demo-ns"},
		ResolvedMCPServers: []*registry.MCPServerRunRequest{resolvedReq},
	}

	desired, err := r.buildDesiredState(nil, []*registry.AgentRunRequest{agentReq})
	if err != nil {
		t.Fatalf("buildDesiredState failed: %v", err)
	}
	if len(desired.Agents) != 1 {
		t.Fatalf("expected 1 agent in desired state, got %d", len(desired.Agents))
	}
	if len(desired.MCPServers) != 1 {
		t.Fatalf("expected 1 resolved mcp server in desired state, got %d", len(desired.MCPServers))
	}
	if desired.Agents[0].DeploymentID != "dep-agent-123" {
		t.Fatalf("expected agent deployment id dep-agent-123, got %q", desired.Agents[0].DeploymentID)
	}
	if desired.MCPServers[0].DeploymentID != "dep-agent-123" {
		t.Fatalf("expected resolved mcp deployment id dep-agent-123, got %q", desired.MCPServers[0].DeploymentID)
	}
	if desired.MCPServers[0].Namespace != "demo-ns" {
		t.Fatalf("expected resolved mcp namespace demo-ns, got %q", desired.MCPServers[0].Namespace)
	}
	if len(desired.Agents[0].ResolvedMCPServers) != 1 {
		t.Fatalf("expected 1 resolved mcp config on agent, got %d", len(desired.Agents[0].ResolvedMCPServers))
	}
	if desired.Agents[0].ResolvedMCPServers[0].Name != "io-github-estruyf-vscode-demo-time-dep-agent-123" {
		t.Fatalf("expected deployment-scoped resolved config name, got %q", desired.Agents[0].ResolvedMCPServers[0].Name)
	}
}
