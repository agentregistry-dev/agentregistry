package kagent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
)

func TestTranslateRuntimeConfig_AgentOnly(t *testing.T) {
	translator := NewTranslator()
	ctx := context.Background()

	fileName := "test-agent"
	fileVersion := "v1"

	desired := &api.DesiredState{
		Agents: []*api.Agent{
			{
				Name:    fileName,
				Version: fileVersion,
				Deployment: api.AgentDeployment{
					Image: "agent-image:latest",
					Env: map[string]string{
						"ENV_VAR": "value",
					},
				},
			},
		},
	}

	config, err := translator.TranslateRuntimeConfig(ctx, desired)
	if err != nil {
		t.Fatalf("TranslateRuntimeConfig failed: %v", err)
	}

	if len(config.Kubernetes.Agents) != 1 {
		t.Fatalf("Expected 1 Agent, got %d", len(config.Kubernetes.Agents))
	}

	agent := config.Kubernetes.Agents[0]
	if agent.Name != "test-agent-v1" {
		t.Errorf("Expected agent name test-agent-v1, got %s", agent.Name)
	}

	// Verify no config maps or volumes for simple agent
	if len(config.Kubernetes.ConfigMaps) != 0 {
		t.Errorf("Expected 0 ConfigMaps, got %d", len(config.Kubernetes.ConfigMaps))
	}

	volumes := agent.Spec.BYO.Deployment.Volumes
	if len(volumes) != 0 {
		t.Errorf("Expected 0 volumes, got %d", len(volumes))
	}
}

func TestTranslateRuntimeConfig_RemoteMCP(t *testing.T) {
	translator := NewTranslator()
	ctx := context.Background()

	desired := &api.DesiredState{
		MCPServers: []*api.MCPServer{
			{
				Name:          "remote-server",
				MCPServerType: api.MCPServerTypeRemote,
				Remote: &api.RemoteMCPServer{
					Host: "example.com",
					Port: 8080,
					Path: "/mcp",
				},
			},
		},
	}

	config, err := translator.TranslateRuntimeConfig(ctx, desired)
	if err != nil {
		t.Fatalf("TranslateRuntimeConfig failed: %v", err)
	}

	if len(config.Kubernetes.RemoteMCPServers) != 1 {
		t.Fatalf("Expected 1 RemoteMCPServer, got %d", len(config.Kubernetes.RemoteMCPServers))
	}

	remote := config.Kubernetes.RemoteMCPServers[0]
	if remote.Name != "remote-server" {
		t.Errorf("Expected name remote-server, got %s", remote.Name)
	}
	expectedURL := "http://example.com:8080/mcp"
	if remote.Spec.URL != expectedURL {
		t.Errorf("Expected URL %s, got %s", expectedURL, remote.Spec.URL)
	}
}

func TestTranslateRuntimeConfig_LocalMCP(t *testing.T) {
	translator := NewTranslator()
	ctx := context.Background()

	desired := &api.DesiredState{
		MCPServers: []*api.MCPServer{
			{
				Name:          "local-server",
				MCPServerType: api.MCPServerTypeLocal,
				Local: &api.LocalMCPServer{
					TransportType: api.TransportTypeHTTP,
					Deployment: api.MCPServerDeployment{
						Image: "mcp-image:latest",
						Env: map[string]string{
							"KAGENT_NAMESPACE": "custom-ns",
						},
					},
					HTTP: &api.HTTPTransport{
						Port: 3000,
						Path: "/sse",
					},
				},
			},
		},
	}

	config, err := translator.TranslateRuntimeConfig(ctx, desired)
	if err != nil {
		t.Fatalf("TranslateRuntimeConfig failed: %v", err)
	}

	if len(config.Kubernetes.MCPServers) != 1 {
		t.Fatalf("Expected 1 MCPServer, got %d", len(config.Kubernetes.MCPServers))
	}

	server := config.Kubernetes.MCPServers[0]
	if server.Name != "local-server" {
		t.Errorf("Expected name local-server, got %s", server.Name)
	}
	// Verify namespace override from env
	if server.Namespace != "custom-ns" {
		t.Errorf("Expected namespace custom-ns, got %s", server.Namespace)
	}

	if server.Spec.TransportType != "http" {
		t.Errorf("Expected transport http, got %s", server.Spec.TransportType)
	}
}

func TestTranslateRuntimeConfig_AgentWithMCPServers(t *testing.T) {
	translator := NewTranslator()
	ctx := context.Background()

	agentName := "test-agent"
	agentVersion := "v1"

	desired := &api.DesiredState{
		Agents: []*api.Agent{
			{
				Name:    agentName,
				Version: agentVersion,
				Deployment: api.AgentDeployment{
					Image: "agent-image:latest",
					Env: map[string]string{
						"ENV_VAR": "value",
					},
				},
				ResolvedMCPServers: []api.ResolvedMCPServerConfig{
					{
						Name: "sqlite",
						Type: "command",
					},
					{
						Name: "brave-search",
						Type: "remote",
						URL:  "http://brave-search:8080/mcp",
						Headers: map[string]string{
							"X-Custom": "header-value",
						},
					},
				},
			},
		},
	}

	config, err := translator.TranslateRuntimeConfig(ctx, desired)
	if err != nil {
		t.Fatalf("TranslateRuntimeConfig failed: %v", err)
	}

	// Verify Kubernetes config type
	if config.Type != api.RuntimeConfigTypeKubernetes {
		t.Errorf("Expected config type Kubernetes, got %s", config.Type)
	}
	if config.Kubernetes == nil {
		t.Fatal("Kubernetes config is nil")
	}

	// 1. Verify ConfigMap generation
	if len(config.Kubernetes.ConfigMaps) != 1 {
		t.Fatalf("Expected 1 ConfigMap, got %d", len(config.Kubernetes.ConfigMaps))
	}

	cm := config.Kubernetes.ConfigMaps[0]
	expectedCMName := "test-agent-v1-mcp-config"
	if cm.Name != expectedCMName {
		t.Errorf("Expected ConfigMap name %s, got %s", expectedCMName, cm.Name)
	}

	// Check JSON content
	jsonContent, ok := cm.Data["mcp-servers.json"]
	if !ok {
		t.Fatal("ConfigMap missing 'mcp-servers.json' key")
	}

	var savedConfigs []api.ResolvedMCPServerConfig
	if err := json.Unmarshal([]byte(jsonContent), &savedConfigs); err != nil {
		t.Fatalf("Failed to decode mcp-servers.json: %v", err)
	}

	if len(savedConfigs) != 2 {
		t.Errorf("Expected 2 saved MCP configs, got %d", len(savedConfigs))
	}
	if savedConfigs[1].URL != "http://brave-search:8080/mcp" {
		t.Errorf("Unexpected URL in saved config: %s", savedConfigs[1].URL)
	}

	// 2. Verify Agent Volume Mounts
	if len(config.Kubernetes.Agents) != 1 {
		t.Fatalf("Expected 1 Agent, got %d", len(config.Kubernetes.Agents))
	}

	agentCR := config.Kubernetes.Agents[0]
	byoSpec := agentCR.Spec.BYO.Deployment

	// Check Volume
	var foundVol bool
	for _, vol := range byoSpec.Volumes {
		if vol.Name == "mcp-config" {
			foundVol = true
			if vol.ConfigMap.Name != expectedCMName {
				t.Errorf("Agent volume pointing to wrong ConfigMap. Expected %s, got %s", expectedCMName, vol.ConfigMap.Name)
			}
		}
	}
	if !foundVol {
		t.Error("Agent spec missing 'mcp-config' volume")
	}

	// Check VolumeMount
	var foundMount bool
	for _, mount := range byoSpec.VolumeMounts {
		if mount.Name == "mcp-config" && mount.MountPath == "/config" {
			foundMount = true
		}
	}
	if !foundMount {
		t.Error("Agent spec missing '/config' volume mount")
	}
}
