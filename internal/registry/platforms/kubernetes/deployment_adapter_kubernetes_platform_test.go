package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	kmcpv1alpha1 "github.com/kagent-dev/kmcp/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
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
				Host: "example.com",
				Port: 8080,
				Path: "/mcp",
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
	if remote.Spec.URL != "http://example.com:8080/mcp" {
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

	desired := &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:    "test-agent",
			Version: "v1",
			Deployment: platformtypes.AgentDeployment{
				Image: "agent-image:latest",
				Env:   map[string]string{"ENV_VAR": "value"},
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
	if len(config.ConfigMaps) != 1 {
		t.Fatalf("expected 1 ConfigMap, got %d", len(config.ConfigMaps))
	}
	cm := config.ConfigMaps[0]
	expectedCMName := "test-agent-v1-mcp-config"
	if cm.Name != expectedCMName {
		t.Errorf("expected ConfigMap name %s, got %s", expectedCMName, cm.Name)
	}

	jsonContent, ok := cm.Data["mcp-servers.json"]
	if !ok {
		t.Fatal("ConfigMap missing mcp-servers.json key")
	}
	var savedConfigs []platformtypes.ResolvedMCPServerConfig
	if err := json.Unmarshal([]byte(jsonContent), &savedConfigs); err != nil {
		t.Fatalf("failed to decode mcp-servers.json: %v", err)
	}
	if len(savedConfigs) != 2 {
		t.Errorf("expected 2 saved MCP configs, got %d", len(savedConfigs))
	}
	if savedConfigs[1].URL != "http://brave-search:8080/mcp" {
		t.Errorf("unexpected URL in saved config: %s", savedConfigs[1].URL)
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
							Host: "remote-mcp.example.com",
							Port: 8080,
							Path: "/mcp",
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
				Host: "example.com",
				Port: 80,
				Path: "/mcp",
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
	configMap := config.ConfigMaps[0]
	if configMap.Name != "demo-agent-1-0-0-mcp-config-dep-agent-123" {
		t.Fatalf("unexpected configmap name: %s", configMap.Name)
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

func TestKubernetesRESTConfig_UsesProviderSpecificKubeconfigContext(t *testing.T) {
	provider := &models.Provider{
		ID:       "kube-b",
		Platform: "kubernetes",
		Config: map[string]any{
			"kubeconfig": testKubernetesProviderKubeconfig(map[string]string{
				"ctx-a": "https://cluster-a.example.test",
				"ctx-b": "https://cluster-b.example.test",
			}, "ctx-a"),
			"context": "ctx-b",
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

func TestDeploymentNamespace_UsesProviderNamespaceWhenDeploymentOmitsIt(t *testing.T) {
	deployment := &models.Deployment{
		ProviderID:   "kubernetes-default",
		ResourceType: "agent",
		Env:          map[string]string{},
	}
	provider := &models.Provider{
		ID:       "kubernetes-default",
		Platform: "kubernetes",
		Config: map[string]any{
			"namespace": "provider-namespace",
		},
	}

	got := deploymentNamespace(deployment, provider)
	if got != "provider-namespace" {
		t.Fatalf("deploymentNamespace() = %q, want %q", got, "provider-namespace")
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

func TestKubernetesDiscoverDeployments_RecordsNamespaceInProviderMetadata(t *testing.T) {
	fakeClient := fake.NewClientBuilder().WithScheme(kubernetesScheme).WithObjects(
		&v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "shared-agent", Namespace: "team-a"}},
		&v1alpha2.Agent{ObjectMeta: metav1.ObjectMeta{Name: "shared-agent", Namespace: "team-b"}},
	).Build()

	originalAmbientRESTConfig := kubernetesGetAmbientRESTConfig
	originalNewClientForConfig := kubernetesNewClientForConfig
	t.Cleanup(func() {
		kubernetesGetAmbientRESTConfig = originalAmbientRESTConfig
		kubernetesNewClientForConfig = originalNewClientForConfig
	})

	kubernetesGetAmbientRESTConfig = func() (*rest.Config, error) {
		return &rest.Config{Host: "https://example.test"}, nil
	}
	kubernetesNewClientForConfig = func(*rest.Config) (client.Client, error) {
		return fakeClient, nil
	}

	discovered, err := kubernetesDiscoverDeployments(context.Background(), &models.Provider{
		ID:       "kubernetes-default",
		Platform: "kubernetes",
	})
	if err != nil {
		t.Fatalf("kubernetesDiscoverDeployments() error = %v", err)
	}
	if len(discovered) != 2 {
		t.Fatalf("expected 2 discovered deployments, got %d", len(discovered))
	}

	namespaces := map[string]struct{}{}
	for _, dep := range discovered {
		meta := models.KubernetesProviderMetadata{}
		if err := dep.ProviderMetadata.UnmarshalInto(&meta); err != nil {
			t.Fatalf("unmarshal provider metadata: %v", err)
		}
		namespaces[meta.Namespace] = struct{}{}
	}
	if _, ok := namespaces["team-a"]; !ok {
		t.Fatalf("expected discovered deployments to include namespace team-a, got %#v", namespaces)
	}
	if _, ok := namespaces["team-b"]; !ok {
		t.Fatalf("expected discovered deployments to include namespace team-b, got %#v", namespaces)
	}
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
