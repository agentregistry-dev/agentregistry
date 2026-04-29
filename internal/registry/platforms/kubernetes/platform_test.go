package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKubernetesTranslatePlatformConfig_AgentOnly(t *testing.T) {
	ctx := context.Background()

	desired := &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:    "test-agent",
			Version: "v1",
			Deployment: platformtypes.AgentDeployment{
				Image: "agent-image:latest",
				Env:   map[string]string{"ENV_VAR": "value"},
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}
	if len(config.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(config.Agents))
	}
	agent := config.Agents[0]
	if agent.Name != "test-agent-v1" {
		t.Errorf("expected agent name test-agent-v1, got %s", agent.Name)
	}
	if len(config.ConfigMaps) != 0 {
		t.Errorf("expected 0 ConfigMaps, got %d", len(config.ConfigMaps))
	}
	if len(agent.Spec.BYO.Deployment.Volumes) != 0 {
		t.Errorf("expected 0 volumes, got %d", len(agent.Spec.BYO.Deployment.Volumes))
	}
}

func TestKubernetesTranslatePlatformConfig_RemoteMCP(t *testing.T) {
	ctx := context.Background()

	desired := &platformtypes.DesiredState{
		MCPServers: []*platformtypes.MCPServer{{
			Name:          "remote-server",
			MCPServerType: platformtypes.MCPServerTypeRemote,
			Remote: &platformtypes.RemoteMCPServer{
				Scheme: "https",
				Host:   "example.com",
				Port:   8080,
				Path:   "/mcp",
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}
	if len(config.RemoteMCPServers) != 1 {
		t.Fatalf("expected 1 RemoteMCPServer, got %d", len(config.RemoteMCPServers))
	}
	remote := config.RemoteMCPServers[0]
	if remote.Name != "remote-server" {
		t.Errorf("expected name remote-server, got %s", remote.Name)
	}
	if remote.Spec.URL != "https://example.com:8080/mcp" {
		t.Errorf("unexpected URL %s", remote.Spec.URL)
	}
}

func TestKubernetesTranslatePlatformConfig_LocalMCP(t *testing.T) {
	ctx := context.Background()

	desired := &platformtypes.DesiredState{
		MCPServers: []*platformtypes.MCPServer{{
			Name:          "local-server",
			MCPServerType: platformtypes.MCPServerTypeLocal,
			Local: &platformtypes.LocalMCPServer{
				TransportType: platformtypes.TransportTypeHTTP,
				Deployment: platformtypes.MCPServerDeployment{
					Image: "mcp-image:latest",
					Env:   map[string]string{"KAGENT_NAMESPACE": "custom-ns"},
				},
				HTTP: &platformtypes.HTTPTransport{
					Port: 3000,
					Path: "/sse",
				},
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}
	if len(config.MCPServers) != 1 {
		t.Fatalf("expected 1 MCPServer, got %d", len(config.MCPServers))
	}
	server := config.MCPServers[0]
	if server.Name != "local-server" {
		t.Errorf("expected name local-server, got %s", server.Name)
	}
	if server.Namespace != "custom-ns" {
		t.Errorf("expected namespace custom-ns, got %s", server.Namespace)
	}
	if server.Spec.TransportType != "http" {
		t.Errorf("expected transport http, got %s", server.Spec.TransportType)
	}
}

func TestKubernetesTranslatePlatformConfig_AgentWithMCPServers(t *testing.T) {
	ctx := context.Background()

	// MCP server config is now injected via MCP_SERVERS_CONFIG env var by ResolveAgent,
	// so the K8s translation layer should not create a ConfigMap for MCP-server-only agents.
	desired := &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:    "test-agent",
			Version: "v1",
			Deployment: platformtypes.AgentDeployment{
				Image: "agent-image:latest",
				Env: map[string]string{
					"ENV_VAR":            "value",
					"MCP_SERVERS_CONFIG": `[{"name":"sqlite","type":"command"},{"name":"brave-search","type":"remote","url":"http://brave-search:8080/mcp","headers":{"X-Custom":"header-value"}}]`,
				},
			},
			ResolvedMCPServers: []platformtypes.ResolvedMCPServerConfig{
				{Name: "sqlite", Type: "command"},
				{
					Name:    "brave-search",
					Type:    "remote",
					URL:     "http://brave-search:8080/mcp",
					Headers: map[string]string{"X-Custom": "header-value"},
				},
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}

	// No ConfigMap should be created -- MCP config is delivered via env var.
	if len(config.ConfigMaps) != 0 {
		t.Fatalf("expected 0 ConfigMaps (MCP config is env-based), got %d", len(config.ConfigMaps))
	}

	// Verify the agent resource was created and MCP_SERVERS_CONFIG env var is present.
	if len(config.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(config.Agents))
	}
	agent := config.Agents[0]
	var foundMCPEnv bool
	for _, env := range agent.Spec.BYO.Deployment.Env {
		if env.Name == "MCP_SERVERS_CONFIG" {
			foundMCPEnv = true
			var savedConfigs []platformtypes.ResolvedMCPServerConfig
			if err := json.Unmarshal([]byte(env.Value), &savedConfigs); err != nil {
				t.Fatalf("failed to decode MCP_SERVERS_CONFIG env: %v", err)
			}
			if len(savedConfigs) != 2 {
				t.Errorf("expected 2 MCP configs in env var, got %d", len(savedConfigs))
			}
			if savedConfigs[1].URL != "http://brave-search:8080/mcp" {
				t.Errorf("unexpected URL in MCP config: %s", savedConfigs[1].URL)
			}
			break
		}
	}
	if !foundMCPEnv {
		t.Error("MCP_SERVERS_CONFIG env var not found on agent")
	}
}

func TestKubernetesTranslatePlatformConfig_NamespaceConsistency(t *testing.T) {
	tests := []struct {
		name              string
		agentEnv          map[string]string
		mcpNamespace      string
		expectedNamespace string
	}{
		{
			name:              "no namespace defaults empty before apply",
			agentEnv:          map[string]string{"SOME_KEY": "some-value"},
			mcpNamespace:      "",
			expectedNamespace: "",
		},
		{
			name:              "explicit namespace propagates",
			agentEnv:          map[string]string{"KAGENT_NAMESPACE": "my-namespace"},
			mcpNamespace:      "my-namespace",
			expectedNamespace: "my-namespace",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := &platformtypes.DesiredState{
				Agents: []*platformtypes.Agent{{
					Name:    "test-agent",
					Version: "v1",
					Deployment: platformtypes.AgentDeployment{
						Image: "agent-image:latest",
						Env:   tt.agentEnv,
					},
					ResolvedMCPServers: []platformtypes.ResolvedMCPServerConfig{
						{Name: "my-mcp", Type: "remote", URL: "http://my-mcp:8080/mcp"},
					},
				}},
				MCPServers: []*platformtypes.MCPServer{
					{
						Name:          "remote-mcp",
						MCPServerType: platformtypes.MCPServerTypeRemote,
						Namespace:     tt.mcpNamespace,
						Remote: &platformtypes.RemoteMCPServer{
							Scheme: "https",
							Host:   "remote-mcp.example.com",
							Port:   8080,
							Path:   "/mcp",
						},
					},
					{
						Name:          "local-mcp",
						MCPServerType: platformtypes.MCPServerTypeLocal,
						Namespace:     tt.mcpNamespace,
						Local: &platformtypes.LocalMCPServer{
							TransportType: platformtypes.TransportTypeHTTP,
							Deployment: platformtypes.MCPServerDeployment{
								Image: "local-mcp:latest",
								Env:   tt.agentEnv,
							},
							HTTP: &platformtypes.HTTPTransport{
								Port: 3000,
								Path: "/mcp",
							},
						},
					},
				},
			}

			config, err := kubernetesTranslatePlatformConfig(context.Background(), desired)
			if err != nil {
				t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
			}
			for _, agent := range config.Agents {
				if agent.Namespace != tt.expectedNamespace {
					t.Errorf("agent namespace = %q, want %q", agent.Namespace, tt.expectedNamespace)
				}
			}
			for _, cm := range config.ConfigMaps {
				if cm.Namespace != tt.expectedNamespace {
					t.Errorf("configmap namespace = %q, want %q", cm.Namespace, tt.expectedNamespace)
				}
			}
			for _, remote := range config.RemoteMCPServers {
				if remote.Namespace != tt.expectedNamespace {
					t.Errorf("remote namespace = %q, want %q", remote.Namespace, tt.expectedNamespace)
				}
			}
			for _, mcp := range config.MCPServers {
				if mcp.Namespace != tt.expectedNamespace {
					t.Errorf("mcp namespace = %q, want %q", mcp.Namespace, tt.expectedNamespace)
				}
			}
		})
	}
}

func TestKubernetesTranslatePlatformConfig_DeploymentIDMetadataAndNaming(t *testing.T) {
	ctx := context.Background()

	desired := &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:         "demo-agent",
			Version:      "1.0.0",
			DeploymentID: "dep-agent-123",
			Deployment: platformtypes.AgentDeployment{
				Image: "agent-image:latest",
				Env:   map[string]string{"KAGENT_NAMESPACE": "demo-ns"},
			},
			ResolvedMCPServers: []platformtypes.ResolvedMCPServerConfig{{Name: "mcp-a", Type: "command"}},
		}},
		MCPServers: []*platformtypes.MCPServer{{
			Name:          "demo-mcp",
			DeploymentID:  "dep-mcp-123",
			MCPServerType: platformtypes.MCPServerTypeRemote,
			Namespace:     "demo-ns",
			Remote: &platformtypes.RemoteMCPServer{
				Scheme: "http",
				Host:   "example.com",
				Port:   80,
				Path:   "/mcp",
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}
	agent := config.Agents[0]
	if agent.Name != "demo-agent-1-0-0-dep-agent-123" {
		t.Fatalf("unexpected agent name: %s", agent.Name)
	}
	if got := agent.Labels[kubernetesDeploymentIDLabelKey]; got != "dep-agent-123" {
		t.Fatalf("agent deployment-id label = %q, want %q", got, "dep-agent-123")
	}
	if got := agent.Annotations[kubernetesDeploymentIDAnnotationKey]; got != "dep-agent-123" {
		t.Fatalf("agent deployment-id annotation = %q, want %q", got, "dep-agent-123")
	}
	// No ConfigMap for MCP-server-only agents (config is now delivered via env var).
	if len(config.ConfigMaps) != 0 {
		t.Fatalf("expected 0 ConfigMaps (MCP config is env-based), got %d", len(config.ConfigMaps))
	}
	remote := config.RemoteMCPServers[0]
	if remote.Name != "demo-mcp-dep-mcp-123" {
		t.Fatalf("unexpected remote mcp name: %s", remote.Name)
	}
}

func TestKubernetesTranslateSkillsForAgent(t *testing.T) {
	t.Run("nil skills returns nil", func(t *testing.T) {
		result, err := kubernetesTranslateSkillsForAgent(nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result != nil {
			t.Fatalf("expected nil, got %+v", result)
		}
	})

	t.Run("git skill parses ref and path from url", func(t *testing.T) {
		result, err := kubernetesTranslateSkillsForAgent([]platformtypes.AgentSkillRef{
			{Name: "parsed-skill", RepoURL: "https://github.com/org/skills/tree/develop/skills/argocd"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		gr := result.GitRefs[0]
		if gr.URL != "https://github.com/org/skills.git" || gr.Ref != "develop" || gr.Path != "skills/argocd" {
			t.Fatalf("unexpected git ref %+v", gr)
		}
	})

	t.Run("duplicate image refs returns error", func(t *testing.T) {
		_, err := kubernetesTranslateSkillsForAgent([]platformtypes.AgentSkillRef{
			{Name: "skill-a", Image: "docker.io/org/skill:latest"},
			{Name: "skill-b", Image: "docker.io/org/skill:latest"},
		})
		if err == nil || !strings.Contains(err.Error(), "duplicate skill image ref") {
			t.Fatalf("unexpected error %v", err)
		}
	})
}

func TestKubernetesTranslatePlatformConfig_AgentWithSkills(t *testing.T) {
	ctx := context.Background()

	desired := &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:    "skilled-agent",
			Version: "v1",
			Deployment: platformtypes.AgentDeployment{
				Image: "agent-image:latest",
				Env:   map[string]string{},
			},
			Skills: []platformtypes.AgentSkillRef{
				{Name: "my-skill", Image: "docker.io/org/my-skill:1.0"},
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}
	agent := config.Agents[0]
	if agent.Spec.Skills == nil || len(agent.Spec.Skills.Refs) != 1 || agent.Spec.Skills.Refs[0] != "docker.io/org/my-skill:1.0" {
		t.Fatalf("unexpected skills %+v", agent.Spec.Skills)
	}
}

func TestKubernetesTranslatePlatformConfig_AgentWithPromptsOnly(t *testing.T) {
	ctx := context.Background()

	desired := &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:    "prompt-agent",
			Version: "v1",
			Deployment: platformtypes.AgentDeployment{
				Image: "agent-image:latest",
				Env:   map[string]string{"KAGENT_NAMESPACE": "test-ns"},
			},
			ResolvedPrompts: []platformtypes.ResolvedPrompt{
				{Name: "system-prompt", Content: "You are a helpful assistant."},
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}
	if len(config.ConfigMaps) != 1 {
		t.Fatalf("expected 1 ConfigMap, got %d", len(config.ConfigMaps))
	}
	cm := config.ConfigMaps[0]
	if _, ok := cm.Data["mcp-servers.json"]; ok {
		t.Error("ConfigMap should not contain mcp-servers.json when no MCP servers are configured")
	}
	promptsJSON, ok := cm.Data["prompts.json"]
	if !ok {
		t.Fatal("ConfigMap missing prompts.json key")
	}
	var savedPrompts []platformtypes.ResolvedPrompt
	if err := json.Unmarshal([]byte(promptsJSON), &savedPrompts); err != nil {
		t.Fatalf("failed to decode prompts.json: %v", err)
	}
	if len(savedPrompts) != 1 || savedPrompts[0].Name != "system-prompt" || savedPrompts[0].Content != "You are a helpful assistant." {
		t.Errorf("unexpected prompts %+v", savedPrompts)
	}

	agent := config.Agents[0]
	if len(agent.Spec.BYO.Deployment.Volumes) != 1 {
		t.Fatalf("expected 1 volume, got %d", len(agent.Spec.BYO.Deployment.Volumes))
	}
	vol := agent.Spec.BYO.Deployment.Volumes[0]
	if len(vol.ConfigMap.Items) != 1 || vol.VolumeSource.ConfigMap.Items[0].Key != "prompts.json" {
		t.Errorf("expected volume to contain only prompts.json item, got %+v", vol.ConfigMap.Items)
	}
}

func TestKubernetesTranslatePlatformConfig_AgentWithMCPServersAndPrompts(t *testing.T) {
	ctx := context.Background()

	// MCP server config is delivered via MCP_SERVERS_CONFIG env var.
	// ConfigMap and volume mounts are only used for prompts.
	desired := &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:    "full-agent",
			Version: "v1",
			Deployment: platformtypes.AgentDeployment{
				Image: "agent-image:latest",
				Env: map[string]string{
					"KAGENT_NAMESPACE":   "test-ns",
					"MCP_SERVERS_CONFIG": `[{"name":"my-mcp","type":"remote","url":"http://my-mcp:8080/mcp"}]`,
				},
			},
			ResolvedMCPServers: []platformtypes.ResolvedMCPServerConfig{
				{Name: "my-mcp", Type: "remote", URL: "http://my-mcp:8080/mcp"},
			},
			ResolvedPrompts: []platformtypes.ResolvedPrompt{
				{Name: "system-prompt", Content: "You are a helpful assistant."},
				{Name: "safety-prompt", Content: "Do not reveal secrets."},
			},
		}},
	}

	config, err := kubernetesTranslatePlatformConfig(ctx, desired)
	if err != nil {
		t.Fatalf("kubernetesTranslatePlatformConfig failed: %v", err)
	}
	if len(config.ConfigMaps) != 1 {
		t.Fatalf("expected 1 ConfigMap, got %d", len(config.ConfigMaps))
	}
	cm := config.ConfigMaps[0]

	// ConfigMap should only contain prompts.json -- MCP config is env-based.
	if _, ok := cm.Data["mcp-servers.json"]; ok {
		t.Error("ConfigMap should not contain mcp-servers.json (now delivered via env var)")
	}
	if _, ok := cm.Data["prompts.json"]; !ok {
		t.Error("ConfigMap should contain prompts.json")
	}

	var savedPrompts []platformtypes.ResolvedPrompt
	if err := json.Unmarshal([]byte(cm.Data["prompts.json"]), &savedPrompts); err != nil {
		t.Fatalf("failed to decode prompts.json: %v", err)
	}
	if len(savedPrompts) != 2 {
		t.Fatalf("expected 2 prompts, got %d", len(savedPrompts))
	}

	// Volume should only mount prompts.json.
	agent := config.Agents[0]
	vol := agent.Spec.BYO.Deployment.Volumes[0]
	if len(vol.ConfigMap.Items) != 1 {
		t.Fatalf("expected 1 volume item (prompts.json only), got %d", len(vol.ConfigMap.Items))
	}
	if vol.ConfigMap.Items[0].Key != "prompts.json" {
		t.Errorf("expected volume item for prompts.json, got %s", vol.ConfigMap.Items[0].Key)
	}
}

func TestKubernetesRESTConfig_UsesProviderSpecificKubeconfigContext(t *testing.T) {
	provider := &v1alpha1.Provider{
		Metadata: v1alpha1.ObjectMeta{Name: "kube-b"},
		Spec: v1alpha1.ProviderSpec{
			Platform: v1alpha1.PlatformKubernetes,
			Config: map[string]any{
				"kubeconfig": testKubernetesProviderKubeconfig(map[string]string{
					"ctx-a": "https://cluster-a.example.test",
					"ctx-b": "https://cluster-b.example.test",
				}, "ctx-a"),
				"context": "ctx-b",
			},
		},
	}

	cfg, err := kubernetesRESTConfig(provider)
	if err != nil {
		t.Fatalf("kubernetesRESTConfig() error = %v", err)
	}
	if cfg.Host != "https://cluster-b.example.test" {
		t.Fatalf("kubernetesRESTConfig() host = %q, want %q", cfg.Host, "https://cluster-b.example.test")
	}
}

func TestKubernetesDeleteAgentResourcesByDeploymentID_RemovesResolvedMCPResources(t *testing.T) {
	const (
		namespace    = "demo-ns"
		deploymentID = "dep-agent-123"
	)

	fakeClient := fake.NewClientBuilder().WithScheme(kubernetesScheme).WithObjects(
		&v1alpha2.Agent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "demo-agent",
				Namespace: namespace,
				Labels:    map[string]string{kubernetesDeploymentIDLabelKey: deploymentID},
			},
		},
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "demo-config",
				Namespace: namespace,
				Labels:    map[string]string{kubernetesDeploymentIDLabelKey: deploymentID},
			},
		},
		&v1alpha2.RemoteMCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "demo-remote-mcp",
				Namespace: namespace,
				Labels:    map[string]string{kubernetesDeploymentIDLabelKey: deploymentID},
			},
		},
		&kmcpv1alpha1.MCPServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "demo-local-mcp",
				Namespace: namespace,
				Labels:    map[string]string{kubernetesDeploymentIDLabelKey: deploymentID},
			},
		},
	).Build()

	err := kubernetesDeleteAgentResourcesByDeploymentID(context.Background(), fakeClient, deploymentID, namespace)
	if err != nil {
		t.Fatalf("kubernetesDeleteAgentResourcesByDeploymentID() error = %v", err)
	}

	assertResourceDeleted := func(obj client.Object, name string) {
		t.Helper()
		key := client.ObjectKey{Name: name, Namespace: namespace}
		if err := fakeClient.Get(context.Background(), key, obj); err == nil {
			t.Fatalf("expected %T %q to be deleted", obj, name)
		}
	}

	assertResourceDeleted(&v1alpha2.Agent{}, "demo-agent")
	assertResourceDeleted(&corev1.ConfigMap{}, "demo-config")
	assertResourceDeleted(&v1alpha2.RemoteMCPServer{}, "demo-remote-mcp")
	assertResourceDeleted(&kmcpv1alpha1.MCPServer{}, "demo-local-mcp")
}

func testKubernetesProviderKubeconfig(contextHosts map[string]string, currentContext string) string {
	clusters := make([]string, 0, len(contextHosts))
	contexts := make([]string, 0, len(contextHosts))
	users := make([]string, 0, len(contextHosts))
	for contextName, host := range contextHosts {
		clusterName := contextName + "-cluster"
		userName := contextName + "-user"
		clusters = append(clusters, fmt.Sprintf(`
  - name: %s
    cluster:
      server: %s
      insecure-skip-tls-verify: true`, clusterName, host))
		contexts = append(contexts, fmt.Sprintf(`
  - name: %s
    context:
      cluster: %s
      user: %s
      namespace: %s-ns`, contextName, clusterName, userName, contextName))
		users = append(users, fmt.Sprintf(`
  - name: %s
    user:
      token: test-token`, userName))
	}

	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:%s
contexts:%s
current-context: %s
users:%s
`, strings.Join(clusters, ""), strings.Join(contexts, ""), currentContext, strings.Join(users, ""))
}

func TestKubernetesDeploymentScopedName_UsesShortUUIDSuffixAndMaxLength(t *testing.T) {
	deploymentID := "2d6d0c54-f8d5-4fc5-908f-f0ae5744871b"

	agentName := kubernetesAgentResourceName("manualk8sdel1772656991", "latest", deploymentID)
	if agentName != "manualk8sdel1772656991-latest-2d6d0c54" {
		t.Fatalf("unexpected agent name: %s", agentName)
	}
	if len(agentName) > maxKubernetesNameLength {
		t.Fatalf("agent name exceeds %d chars: %d (%s)", maxKubernetesNameLength, len(agentName), agentName)
	}
	if strings.Contains(agentName, deploymentID) {
		t.Fatalf("agent name should not include full uuid suffix: %s", agentName)
	}

	configMapName := kubernetesAgentConfigMapName("manualk8sdel1772656991", "latest", deploymentID)
	if !strings.HasSuffix(configMapName, "-2d6d0c54") {
		t.Fatalf("configmap name should end with short uuid suffix: %s", configMapName)
	}
	if len(configMapName) > maxKubernetesNameLength {
		t.Fatalf("configmap name exceeds %d chars: %d (%s)", maxKubernetesNameLength, len(configMapName), configMapName)
	}
}

func TestKubernetesDeploymentScopedName_TruncatesLongBaseButPreservesSuffix(t *testing.T) {
	deploymentID := "2d6d0c54-f8d5-4fc5-908f-f0ae5744871b"
	longName := strings.Repeat("verylongagentname", 6)

	got := kubernetesAgentResourceName(longName, "latest", deploymentID)
	if len(got) > maxKubernetesNameLength {
		t.Fatalf("name exceeds %d chars: %d (%s)", maxKubernetesNameLength, len(got), got)
	}
	if !strings.HasSuffix(got, "-2d6d0c54") {
		t.Fatalf("expected uuid short suffix to be preserved, got %s", got)
	}
}
