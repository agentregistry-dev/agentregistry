package kagent

import (
	"context"
	"encoding/json"
	"strings"
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

	tests := []struct {
		name        string
		remote      *api.RemoteMCPServer
		expectedURL string
	}{
		{
			name: "http with explicit port",
			remote: &api.RemoteMCPServer{
				Scheme: "http",
				Host:   "example.com",
				Port:   8080,
				Path:   "/mcp",
			},
			expectedURL: "http://example.com:8080/mcp",
		},
		{
			name: "https with default port omitted",
			remote: &api.RemoteMCPServer{
				Scheme: "https",
				Host:   "example.com",
				Port:   443,
				Path:   "/mcp",
			},
			expectedURL: "https://example.com/mcp",
		},
		{
			name: "http with default port omitted",
			remote: &api.RemoteMCPServer{
				Scheme: "http",
				Host:   "example.com",
				Port:   80,
				Path:   "/mcp",
			},
			expectedURL: "http://example.com/mcp",
		},
		{
			name: "https with non-default port",
			remote: &api.RemoteMCPServer{
				Scheme: "https",
				Host:   "my-workspace.cloud.databricks.com",
				Port:   8443,
				Path:   "/mcp",
			},
			expectedURL: "https://my-workspace.cloud.databricks.com:8443/mcp",
		},
		{
			name: "empty scheme defaults to http",
			remote: &api.RemoteMCPServer{
				Host: "example.com",
				Port: 8080,
				Path: "/mcp",
			},
			expectedURL: "http://example.com:8080/mcp",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := &api.DesiredState{
				MCPServers: []*api.MCPServer{
					{
						Name:          "remote-server",
						MCPServerType: api.MCPServerTypeRemote,
						Remote:        tt.remote,
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
			if remote.Spec.URL != tt.expectedURL {
				t.Errorf("Expected URL %s, got %s", tt.expectedURL, remote.Spec.URL)
			}
		})
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
							Scheme: "http",
							Host:   "remote-mcp.example.com",
							Port:   8080,
							Path:   "/mcp",
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
					Scheme: "http",
					Host:   "example.com",
					Port:   80,
					Path:   "/mcp",
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

func TestTranslateSkillsForAgent(t *testing.T) {
	t.Run("nil skills returns nil", func(t *testing.T) {
		result, err := translateSkillsForAgent(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Fatalf("expected nil, got %+v", result)
		}
	})

	t.Run("empty skills returns nil", func(t *testing.T) {
		result, err := translateSkillsForAgent([]api.AgentSkillRef{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Fatalf("expected nil, got %+v", result)
		}
	})

	t.Run("docker image skills populate Refs", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "skill-a", Image: "docker.io/org/skill-a:latest"},
			{Name: "skill-b", Image: "ghcr.io/org/skill-b:1.0"},
		}
		result, err := translateSkillsForAgent(skills)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Refs) != 2 {
			t.Fatalf("expected 2 refs, got %d", len(result.Refs))
		}
		if result.Refs[0] != "docker.io/org/skill-a:latest" {
			t.Errorf("refs[0] = %q, want %q", result.Refs[0], "docker.io/org/skill-a:latest")
		}
		if result.Refs[1] != "ghcr.io/org/skill-b:1.0" {
			t.Errorf("refs[1] = %q, want %q", result.Refs[1], "ghcr.io/org/skill-b:1.0")
		}
	})

	t.Run("git skill with explicit ref and path", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "git-skill", RepoURL: "https://github.com/org/skill", Ref: "main", Path: "skills/my-skill"},
		}
		result, err := translateSkillsForAgent(skills)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.GitRefs) != 1 {
			t.Fatalf("expected 1 gitRef, got %d", len(result.GitRefs))
		}
		gr := result.GitRefs[0]
		if gr.URL != "https://github.com/org/skill.git" {
			t.Errorf("gitRef URL = %q, want %q", gr.URL, "https://github.com/org/skill.git")
		}
		if gr.Name != "git-skill" {
			t.Errorf("gitRef Name = %q, want %q", gr.Name, "git-skill")
		}
		// Explicit ref/path on AgentSkillRef take precedence
		if gr.Ref != "main" {
			t.Errorf("gitRef Ref = %q, want %q", gr.Ref, "main")
		}
		if gr.Path != "skills/my-skill" {
			t.Errorf("gitRef Path = %q, want %q", gr.Path, "skills/my-skill")
		}
	})

	t.Run("git skill parses ref and path from URL", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "parsed-skill", RepoURL: "https://github.com/org/skills/tree/develop/skills/argocd"},
		}
		result, err := translateSkillsForAgent(skills)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.GitRefs) != 1 {
			t.Fatalf("expected 1 gitRef, got %d", len(result.GitRefs))
		}
		gr := result.GitRefs[0]
		if gr.URL != "https://github.com/org/skills.git" {
			t.Errorf("gitRef URL = %q, want %q", gr.URL, "https://github.com/org/skills.git")
		}
		if gr.Ref != "develop" {
			t.Errorf("gitRef Ref = %q, want %q", gr.Ref, "develop")
		}
		if gr.Path != "skills/argocd" {
			t.Errorf("gitRef Path = %q, want %q", gr.Path, "skills/argocd")
		}
	})

	t.Run("empty skill name returns error", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{RepoURL: "https://github.com/org/skills/tree/main/skill-1"},
		}
		_, err := translateSkillsForAgent(skills)
		if err == nil {
			t.Fatal("expected error for empty skill name")
		}
		if !strings.Contains(err.Error(), "skill name is required") {
			t.Errorf("error = %q, want it to contain 'skill name is required'", err.Error())
		}
	})

	t.Run("duplicate image refs returns error", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "skill-a", Image: "docker.io/org/skill:latest"},
			{Name: "skill-b", Image: "docker.io/org/skill:latest"},
		}
		_, err := translateSkillsForAgent(skills)
		if err == nil {
			t.Fatal("expected error for duplicate image ref")
		}
		if !strings.Contains(err.Error(), "duplicate skill image ref") {
			t.Errorf("error = %q, want it to contain 'duplicate skill image ref'", err.Error())
		}
	})

	t.Run("duplicate git names returns error", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "skill", RepoURL: "https://github.com/org/skill"},
			{Name: "skill", RepoURL: "https://github.com/other-org/skill"},
		}
		_, err := translateSkillsForAgent(skills)
		if err == nil {
			t.Fatal("expected error for duplicate git name")
		}
		if !strings.Contains(err.Error(), "duplicate skill git name") {
			t.Errorf("error = %q, want it to contain 'duplicate skill git name'", err.Error())
		}
	})

	t.Run("same repo different names is allowed", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "skill-a", RepoURL: "https://github.com/org/skill/tree/main/a"},
			{Name: "skill-b", RepoURL: "https://github.com/org/skill/tree/main/b"},
		}
		result, err := translateSkillsForAgent(skills)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil || len(result.GitRefs) != 2 {
			t.Fatalf("expected 2 gitRefs, got %v", result)
		}
	})

	t.Run("invalid repo URL returns error", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "bad-skill", RepoURL: "https://gitlab.com/org/skill"},
		}
		_, err := translateSkillsForAgent(skills)
		if err == nil {
			t.Fatal("expected error for non-github URL")
		}
	})

	t.Run("mixed skills populates both Refs and GitRefs", func(t *testing.T) {
		skills := []api.AgentSkillRef{
			{Name: "docker-skill", Image: "docker.io/org/skill:latest"},
			{Name: "git-skill", RepoURL: "https://github.com/org/skill"},
		}
		result, err := translateSkillsForAgent(skills)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if len(result.Refs) != 1 {
			t.Fatalf("expected 1 ref, got %d", len(result.Refs))
		}
		if result.Refs[0] != "docker.io/org/skill:latest" {
			t.Errorf("refs[0] = %q, want %q", result.Refs[0], "docker.io/org/skill:latest")
		}
		if len(result.GitRefs) != 1 {
			t.Fatalf("expected 1 gitRef, got %d", len(result.GitRefs))
		}
		if result.GitRefs[0].URL != "https://github.com/org/skill.git" {
			t.Errorf("gitRef URL = %q, want %q", result.GitRefs[0].URL, "https://github.com/org/skill.git")
		}
	})
}

func TestTranslateRuntimeConfig_AgentWithSkills(t *testing.T) {
	translator := NewTranslator()
	ctx := context.Background()

	desired := &api.DesiredState{
		Agents: []*api.Agent{
			{
				Name:    "skilled-agent",
				Version: "v1",
				Deployment: api.AgentDeployment{
					Image: "agent-image:latest",
					Env:   map[string]string{},
				},
				Skills: []api.AgentSkillRef{
					{Name: "my-skill", Image: "docker.io/org/my-skill:1.0"},
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
	if agent.Spec.Skills == nil {
		t.Fatal("Expected Skills to be set on agent spec")
	}
	if len(agent.Spec.Skills.Refs) != 1 {
		t.Fatalf("Expected 1 skill ref, got %d", len(agent.Spec.Skills.Refs))
	}
	if agent.Spec.Skills.Refs[0] != "docker.io/org/my-skill:1.0" {
		t.Errorf("skill ref = %q, want %q", agent.Spec.Skills.Refs[0], "docker.io/org/my-skill:1.0")
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
