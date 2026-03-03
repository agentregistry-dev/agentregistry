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

// TestTranslateRuntimeConfig_NamespaceConsistency verifies that agents, MCP servers,
// and ConfigMaps all deploy to the same namespace.
func TestTranslateRuntimeConfig_NamespaceConsistency(t *testing.T) {
	tests := []struct {
		name              string
		agentEnv          map[string]string
		mcpNamespace      string // Namespace field on the MCPServer
		expectedNamespace string
	}{
		{
			name:              "no namespace provided defaults to '' for all resources",
			agentEnv:          map[string]string{"SOME_KEY": "some-value"},
			mcpNamespace:      "",
			expectedNamespace: "",
		},
		{
			name:              "explicit namespace via KAGENT_NAMESPACE propagates to all resources",
			agentEnv:          map[string]string{"KAGENT_NAMESPACE": "my-namespace"},
			mcpNamespace:      "my-namespace",
			expectedNamespace: "my-namespace",
		},
		{
			name:              "custom namespace via KAGENT_NAMESPACE",
			agentEnv:          map[string]string{"KAGENT_NAMESPACE": "production"},
			mcpNamespace:      "production",
			expectedNamespace: "production",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := NewTranslator()
			ctx := context.Background()

			desired := &api.DesiredState{
				Agents: []*api.Agent{
					{
						Name:    "test-agent",
						Version: "v1",
						Deployment: api.AgentDeployment{
							Image: "agent-image:latest",
							Env:   tt.agentEnv,
						},
						ResolvedMCPServers: []api.ResolvedMCPServerConfig{
							{Name: "my-mcp", Type: "remote", URL: "http://my-mcp:8080/mcp"},
						},
					},
				},
				MCPServers: []*api.MCPServer{
					{
						Name:          "remote-mcp",
						MCPServerType: api.MCPServerTypeRemote,
						Namespace:     tt.mcpNamespace,
						Remote: &api.RemoteMCPServer{
							Host: "remote-mcp.example.com",
							Port: 8080,
							Path: "/mcp",
						},
					},
					{
						Name:          "local-mcp",
						MCPServerType: api.MCPServerTypeLocal,
						Namespace:     tt.mcpNamespace,
						Local: &api.LocalMCPServer{
							TransportType: api.TransportTypeHTTP,
							Deployment: api.MCPServerDeployment{
								Image: "local-mcp:latest",
								Env:   tt.agentEnv,
							},
							HTTP: &api.HTTPTransport{
								Port: 3000,
								Path: "/mcp",
							},
						},
					},
				},
			}

			config, err := translator.TranslateRuntimeConfig(ctx, desired)
			if err != nil {
				t.Fatalf("TranslateRuntimeConfig failed: %v", err)
			}

			// Collect all namespaces from every generated resource
			type nsCheck struct {
				kind      string
				name      string
				namespace string
			}
			var checks []nsCheck

			for _, a := range config.Kubernetes.Agents {
				checks = append(checks, nsCheck{"Agent", a.Name, a.Namespace})
			}
			for _, cm := range config.Kubernetes.ConfigMaps {
				checks = append(checks, nsCheck{"ConfigMap", cm.Name, cm.Namespace})
			}
			for _, r := range config.Kubernetes.RemoteMCPServers {
				checks = append(checks, nsCheck{"RemoteMCPServer", r.Name, r.Namespace})
			}
			for _, m := range config.Kubernetes.MCPServers {
				checks = append(checks, nsCheck{"MCPServer", m.Name, m.Namespace})
			}

			// Verify we produced all expected resource types
			expectedCounts := map[string]int{"Agent": 1, "ConfigMap": 1, "RemoteMCPServer": 1, "MCPServer": 1}
			actualCounts := make(map[string]int)
			for _, c := range checks {
				actualCounts[c.kind]++
			}
			for kind, want := range expectedCounts {
				if got := actualCounts[kind]; got != want {
					t.Errorf("expected %d %s resource(s), got %d", want, kind, got)
				}
			}

			// All resources must have the same namespace
			for _, c := range checks {
				if c.namespace != tt.expectedNamespace {
					t.Errorf("%s %q namespace = %q, want %q",
						c.kind, c.name, c.namespace, tt.expectedNamespace)
				}
			}
		})
	}
}

func TestTranslateRuntimeConfig_DeploymentIDMetadataAndNaming(t *testing.T) {
	translator := NewTranslator()
	ctx := context.Background()

	desired := &api.DesiredState{
		Agents: []*api.Agent{
			{
				Name:         "demo-agent",
				Version:      "1.0.0",
				DeploymentID: "dep-agent-123",
				Deployment: api.AgentDeployment{
					Image: "agent-image:latest",
					Env: map[string]string{
						"KAGENT_NAMESPACE": "demo-ns",
					},
				},
				ResolvedMCPServers: []api.ResolvedMCPServerConfig{
					{Name: "mcp-a", Type: "command"},
				},
			},
		},
		MCPServers: []*api.MCPServer{
			{
				Name:          "demo-mcp",
				DeploymentID:  "dep-mcp-123",
				MCPServerType: api.MCPServerTypeRemote,
				Namespace:     "demo-ns",
				Remote: &api.RemoteMCPServer{
					Host: "example.com",
					Port: 80,
					Path: "/mcp",
				},
			},
		},
	}

	config, err := translator.TranslateRuntimeConfig(ctx, desired)
	if err != nil {
		t.Fatalf("TranslateRuntimeConfig failed: %v", err)
	}

	if len(config.Kubernetes.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(config.Kubernetes.Agents))
	}
	agent := config.Kubernetes.Agents[0]
	if agent.Name != "demo-agent-1-0-0-dep-agent-123" {
		t.Fatalf("unexpected agent name: %s", agent.Name)
	}
	if got := agent.Labels[DeploymentIDLabelKey]; got != "dep-agent-123" {
		t.Fatalf("agent deployment-id label = %q, want %q", got, "dep-agent-123")
	}
	if got := agent.Annotations[DeploymentIDAnnotationKey]; got != "dep-agent-123" {
		t.Fatalf("agent deployment-id annotation = %q, want %q", got, "dep-agent-123")
	}

	if len(config.Kubernetes.ConfigMaps) != 1 {
		t.Fatalf("expected 1 configmap, got %d", len(config.Kubernetes.ConfigMaps))
	}
	configMap := config.Kubernetes.ConfigMaps[0]
	if configMap.Name != "demo-agent-1-0-0-mcp-config-dep-agent-123" {
		t.Fatalf("unexpected configmap name: %s", configMap.Name)
	}
	if got := configMap.Labels[DeploymentIDLabelKey]; got != "dep-agent-123" {
		t.Fatalf("configmap deployment-id label = %q, want %q", got, "dep-agent-123")
	}

	if len(config.Kubernetes.RemoteMCPServers) != 1 {
		t.Fatalf("expected 1 remote mcp server, got %d", len(config.Kubernetes.RemoteMCPServers))
	}
	remote := config.Kubernetes.RemoteMCPServers[0]
	if remote.Name != "demo-mcp-dep-mcp-123" {
		t.Fatalf("unexpected remote mcp name: %s", remote.Name)
	}
	if got := remote.Labels[DeploymentIDLabelKey]; got != "dep-mcp-123" {
		t.Fatalf("remote mcp deployment-id label = %q, want %q", got, "dep-mcp-123")
	}
}

func TestTranslateRuntimeConfig_DuplicateArtifactIdentityUsesDistinctDeploymentScopedNames(t *testing.T) {
	translator := NewTranslator()
	ctx := context.Background()

	desired := &api.DesiredState{
		Agents: []*api.Agent{
			{
				Name:         "com.example/planner",
				Version:      "1.0.0",
				DeploymentID: "dep-old",
				Deployment: api.AgentDeployment{
					Image: "agent-image:latest",
					Env: map[string]string{
						"KAGENT_NAMESPACE": "demo-ns",
					},
				},
				ResolvedMCPServers: []api.ResolvedMCPServerConfig{
					{Name: "tools", Type: "command"},
				},
			},
			{
				Name:         "com.example/planner",
				Version:      "1.0.0",
				DeploymentID: "dep-new",
				Deployment: api.AgentDeployment{
					Image: "agent-image:latest",
					Env: map[string]string{
						"KAGENT_NAMESPACE": "demo-ns",
					},
				},
				ResolvedMCPServers: []api.ResolvedMCPServerConfig{
					{Name: "tools", Type: "command"},
				},
			},
		},
	}

	config, err := translator.TranslateRuntimeConfig(ctx, desired)
	if err != nil {
		t.Fatalf("TranslateRuntimeConfig failed: %v", err)
	}

	if len(config.Kubernetes.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(config.Kubernetes.Agents))
	}
	if len(config.Kubernetes.ConfigMaps) != 2 {
		t.Fatalf("expected 2 configmaps, got %d", len(config.Kubernetes.ConfigMaps))
	}

	agentNames := map[string]struct{}{}
	for _, agent := range config.Kubernetes.Agents {
		agentNames[agent.Name] = struct{}{}
	}
	if _, ok := agentNames["com-example-planner-1-0-0-dep-old"]; !ok {
		t.Fatalf("missing deployment-scoped agent name for dep-old: %v", agentNames)
	}
	if _, ok := agentNames["com-example-planner-1-0-0-dep-new"]; !ok {
		t.Fatalf("missing deployment-scoped agent name for dep-new: %v", agentNames)
	}

	configMapNames := map[string]struct{}{}
	for _, cm := range config.Kubernetes.ConfigMaps {
		configMapNames[cm.Name] = struct{}{}
	}
	if _, ok := configMapNames["com-example-planner-1-0-0-mcp-config-dep-old"]; !ok {
		t.Fatalf("missing deployment-scoped configmap name for dep-old: %v", configMapNames)
	}
	if _, ok := configMapNames["com-example-planner-1-0-0-mcp-config-dep-new"]; !ok {
		t.Fatalf("missing deployment-scoped configmap name for dep-new: %v", configMapNames)
	}
}
