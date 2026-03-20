package local

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	composetypes "github.com/compose-spec/compose-go/v2/types"
)

func TestUndeploy_RemovesLocalArtifactsWhenRegistryArtifactIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	deployment := &models.Deployment{
		ID:           "dep-local-123",
		ServerName:   "io.test/agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "local",
	}

	agent := &platformtypes.Agent{
		Name:         deployment.ServerName,
		Version:      deployment.Version,
		DeploymentID: deployment.ID,
	}
	resolvedServer := &platformtypes.MCPServer{
		Name:          "io.test/dependency",
		DeploymentID:  deployment.ID,
		MCPServerType: platformtypes.MCPServerTypeRemote,
		Remote: &platformtypes.RemoteMCPServer{
			Host: "example.com",
			Port: 443,
			Path: "/mcp",
		},
	}

	agentServiceName := localAgentServiceName(agent)
	resolvedServiceName := localMCPServiceName(resolvedServer)

	err := WriteLocalPlatformFiles(tempDir, &platformtypes.LocalPlatformConfig{
		DockerCompose: &platformtypes.DockerComposeConfig{
			Name:       "test",
			WorkingDir: tempDir,
			Services: map[string]composetypes.ServiceConfig{
				"agent_gateway":     {Name: "agent_gateway"},
				agentServiceName:    {Name: agentServiceName},
				resolvedServiceName: {Name: resolvedServiceName},
				"unrelated-service": {Name: "unrelated-service"},
			},
		},
		AgentGateway: &platformtypes.AgentGatewayConfig{
			Config: struct{}{},
			Binds: []platformtypes.LocalBind{{
				Port: 8080,
				Listeners: []platformtypes.LocalListener{{
					Name:     "default",
					Protocol: platformtypes.LocalListenerProtocolHTTP,
					Routes: []platformtypes.LocalRoute{
						{
							RouteName: localMCPRouteName,
							Backends: []platformtypes.RouteBackend{{
								MCP: &platformtypes.MCPBackend{
									Targets: []platformtypes.MCPTarget{
										{Name: resolvedServiceName},
										{Name: "unrelated-target"},
									},
								},
							}},
						},
						{
							RouteName: agentServiceName + "_route",
							Backends:  []platformtypes.RouteBackend{{Host: agentServiceName + ":8080"}},
						},
						{
							RouteName: "unrelated-route",
							Backends:  []platformtypes.RouteBackend{{Host: "unrelated-service:8080"}},
						},
					},
				}},
			}},
		},
	}, 8080)
	if err != nil {
		t.Fatalf("WriteLocalPlatformFiles() error = %v", err)
	}

	registry := servicetesting.NewFakeRegistry()
	registry.GetAgentByNameAndVersionFn = func(context.Context, string, string) (*models.AgentResponse, error) {
		return nil, database.ErrNotFound
	}

	adapter := NewLocalDeploymentAdapter(registry, tempDir, 8080)

	originalComposeUp := runLocalComposeUp
	originalRefresh := refreshLocalAgentMCPConfig
	originalPromptsRefresh := refreshLocalAgentPromptsConfig
	t.Cleanup(func() {
		runLocalComposeUp = originalComposeUp
		refreshLocalAgentMCPConfig = originalRefresh
		refreshLocalAgentPromptsConfig = originalPromptsRefresh
	})

	runLocalComposeUp = func(context.Context, string, bool) error {
		return nil
	}

	refreshCalled := false
	refreshLocalAgentMCPConfig = func(target *common.MCPConfigTarget, servers []common.PythonMCPServer, verbose bool) error {
		refreshCalled = true
		if target == nil || target.AgentName != deployment.ServerName || target.Version != deployment.Version {
			t.Fatalf("unexpected refresh target: %#v", target)
		}
		if len(servers) != 0 {
			t.Fatalf("expected cleanup refresh with no servers, got %#v", servers)
		}
		if verbose {
			t.Fatal("expected non-verbose cleanup refresh")
		}
		return nil
	}

	promptsRefreshCalled := false
	refreshLocalAgentPromptsConfig = func(target *common.MCPConfigTarget, prompts []common.PythonPrompt, verbose bool) error {
		promptsRefreshCalled = true
		if target == nil || target.AgentName != deployment.ServerName || target.Version != deployment.Version {
			t.Fatalf("unexpected prompts refresh target: %#v", target)
		}
		if len(prompts) != 0 {
			t.Fatalf("expected cleanup refresh with no prompts, got %#v", prompts)
		}
		return nil
	}

	if err := adapter.Undeploy(context.Background(), deployment); err != nil {
		t.Fatalf("Undeploy() error = %v", err)
	}
	if !refreshCalled {
		t.Fatal("expected RefreshMCPConfig cleanup to be called for missing agent undeploy")
	}
	if !promptsRefreshCalled {
		t.Fatal("expected RefreshPromptsConfig cleanup to be called for missing agent undeploy")
	}

	composeCfg, err := LoadLocalDockerComposeConfig(tempDir)
	if err != nil {
		t.Fatalf("LoadLocalDockerComposeConfig() error = %v", err)
	}
	if _, exists := composeCfg.Services[agentServiceName]; exists {
		t.Fatalf("expected agent service %q to be removed", agentServiceName)
	}
	if _, exists := composeCfg.Services[resolvedServiceName]; exists {
		t.Fatalf("expected resolved service %q to be removed", resolvedServiceName)
	}
	if _, exists := composeCfg.Services["unrelated-service"]; !exists {
		t.Fatal("expected unrelated service to remain")
	}

	gatewayCfg, err := LoadLocalAgentGatewayConfig(tempDir, 8080)
	if err != nil {
		t.Fatalf("LoadLocalAgentGatewayConfig() error = %v", err)
	}
	targets := extractMCPRouteTargets(gatewayCfg)
	if len(targets) != 1 || targets[0].Name != "unrelated-target" {
		t.Fatalf("unexpected remaining MCP targets: %#v", targets)
	}
	routes := extractNonMCPRoutes(gatewayCfg)
	if len(routes) != 1 || routes[0].RouteName != "unrelated-route" {
		t.Fatalf("unexpected remaining non-MCP routes: %#v", routes)
	}
}

func TestUndeploy_CallsComposeDownWhenNoServicesRemain(t *testing.T) {
	tempDir := t.TempDir()
	deployment := &models.Deployment{
		ID:           "dep-last-001",
		ServerName:   "io.test/only-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "local",
	}

	agent := &platformtypes.Agent{
		Name:         deployment.ServerName,
		Version:      deployment.Version,
		DeploymentID: deployment.ID,
	}
	agentServiceName := localAgentServiceName(agent)

	err := WriteLocalPlatformFiles(tempDir, &platformtypes.LocalPlatformConfig{
		DockerCompose: &platformtypes.DockerComposeConfig{
			Name:       "test",
			WorkingDir: tempDir,
			Services: map[string]composetypes.ServiceConfig{
				agentServiceName: {Name: agentServiceName},
			},
		},
		AgentGateway: &platformtypes.AgentGatewayConfig{
			Config: struct{}{},
			Binds:  []platformtypes.LocalBind{},
		},
	}, 8080)
	if err != nil {
		t.Fatalf("WriteLocalPlatformFiles() error = %v", err)
	}

	registry := servicetesting.NewFakeRegistry()
	registry.GetAgentByNameAndVersionFn = func(context.Context, string, string) (*models.AgentResponse, error) {
		return nil, database.ErrNotFound
	}

	adapter := NewLocalDeploymentAdapter(registry, tempDir, 8080)

	originalComposeUp := runLocalComposeUp
	originalComposeDown := runLocalComposeDown
	originalRefresh := refreshLocalAgentMCPConfig
	originalPromptsRefresh := refreshLocalAgentPromptsConfig
	t.Cleanup(func() {
		runLocalComposeUp = originalComposeUp
		runLocalComposeDown = originalComposeDown
		refreshLocalAgentMCPConfig = originalRefresh
		refreshLocalAgentPromptsConfig = originalPromptsRefresh
	})

	composeUpCalled := false
	runLocalComposeUp = func(context.Context, string, bool) error {
		composeUpCalled = true
		return nil
	}
	composeDownCalled := false
	runLocalComposeDown = func(context.Context, string, bool) error {
		composeDownCalled = true
		return nil
	}
	refreshLocalAgentMCPConfig = func(*common.MCPConfigTarget, []common.PythonMCPServer, bool) error { return nil }
	refreshLocalAgentPromptsConfig = func(*common.MCPConfigTarget, []common.PythonPrompt, bool) error { return nil }

	if err := adapter.Undeploy(context.Background(), deployment); err != nil {
		t.Fatalf("Undeploy() error = %v", err)
	}
	if composeUpCalled {
		t.Fatal("expected compose up NOT to be called when no services remain")
	}
	if !composeDownCalled {
		t.Fatal("expected compose down to be called when no services remain")
	}
}

func TestDeploy_WritesPromptsConfig(t *testing.T) {
	tempDir := t.TempDir()
	deployment := &models.Deployment{
		ServerName:   "prompt-agent",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   "local",
		Env:          map[string]string{},
	}

	registry := servicetesting.NewFakeRegistry()
	registry.GetAgentByNameAndVersionFn = func(_ context.Context, name, version string) (*models.AgentResponse, error) {
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:  name,
					Image: "agent-image:latest",
				},
				Version: version,
			},
		}, nil
	}
	registry.ResolveAgentManifestPromptsFn = func(_ context.Context, _ *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
		return []platformtypes.ResolvedPrompt{
			{Name: "system-prompt", Content: "You are a helpful assistant."},
		}, nil
	}

	adapter := NewLocalDeploymentAdapter(registry, tempDir, 8080)

	originalComposeUp := runLocalComposeUp
	originalRefresh := refreshLocalAgentMCPConfig
	originalPromptsRefresh := refreshLocalAgentPromptsConfig
	t.Cleanup(func() {
		runLocalComposeUp = originalComposeUp
		refreshLocalAgentMCPConfig = originalRefresh
		refreshLocalAgentPromptsConfig = originalPromptsRefresh
	})

	runLocalComposeUp = func(context.Context, string, bool) error { return nil }
	refreshLocalAgentMCPConfig = func(*common.MCPConfigTarget, []common.PythonMCPServer, bool) error { return nil }

	var capturedPrompts []common.PythonPrompt
	var capturedTarget *common.MCPConfigTarget
	refreshLocalAgentPromptsConfig = func(target *common.MCPConfigTarget, prompts []common.PythonPrompt, _ bool) error {
		capturedTarget = target
		capturedPrompts = prompts
		return nil
	}

	result, err := adapter.Deploy(context.Background(), deployment)
	if err != nil {
		t.Fatalf("Deploy() error = %v", err)
	}
	if result.Status != "deployed" {
		t.Fatalf("expected status deployed, got %s", result.Status)
	}

	if capturedTarget == nil {
		t.Fatal("expected RefreshPromptsConfig to be called")
	}
	if capturedTarget.AgentName != "prompt-agent" {
		t.Fatalf("expected agent name prompt-agent, got %s", capturedTarget.AgentName)
	}
	if len(capturedPrompts) != 1 {
		t.Fatalf("expected 1 prompt, got %d", len(capturedPrompts))
	}
	if capturedPrompts[0].Name != "system-prompt" || capturedPrompts[0].Content != "You are a helpful assistant." {
		t.Fatalf("unexpected prompt %+v", capturedPrompts[0])
	}
}
