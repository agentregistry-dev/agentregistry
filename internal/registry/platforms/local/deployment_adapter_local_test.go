package local

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type fakeLocalPlatformRuntimeRegistry struct {
	getAgentFn        func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	resolveSkillsFn   func(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error)
	resolvePromptsFn  func(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error)
	getServerByVerFn  func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	getProviderByIDFn func(ctx context.Context, providerID string) (*models.Provider, error)
	getPromptByNameFn func(ctx context.Context, promptName string) (*models.PromptResponse, error)
	getPromptByVerFn  func(ctx context.Context, promptName, version string) (*models.PromptResponse, error)
}

func (f *fakeLocalPlatformRuntimeRegistry) ListServers(context.Context, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	return nil, "", nil
}

func (f *fakeLocalPlatformRuntimeRegistry) GetServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if f.getServerByVerFn != nil {
		return f.getServerByVerFn(ctx, serverName, "")
	}
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	if f.getProviderByIDFn != nil {
		return f.getProviderByIDFn(ctx, providerID)
	}
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) GetServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.getServerByVerFn != nil {
		return f.getServerByVerFn(ctx, serverName, version)
	}
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) GetServerVersions(context.Context, string) ([]*apiv0.ServerResponse, error) {
	return nil, nil
}

func (f *fakeLocalPlatformRuntimeRegistry) PublishServer(context.Context, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeLocalPlatformRuntimeRegistry) UpdateServer(context.Context, string, string, *apiv0.ServerJSON, *string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeLocalPlatformRuntimeRegistry) SetServerReadme(context.Context, string, string, []byte, string) error {
	return nil
}

func (f *fakeLocalPlatformRuntimeRegistry) GetLatestServerReadme(context.Context, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) GetServerReadme(context.Context, string, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) DeleteServer(context.Context, string, string) error {
	return nil
}

func (f *fakeLocalPlatformRuntimeRegistry) SetServerEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (f *fakeLocalPlatformRuntimeRegistry) GetServerEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) ListAgents(context.Context, *database.AgentFilter, string, int) ([]*models.AgentResponse, string, error) {
	return nil, "", nil
}

func (f *fakeLocalPlatformRuntimeRegistry) GetAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if f.getAgentFn != nil {
		return f.getAgentFn(ctx, agentName, "")
	}
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.getAgentFn != nil {
		return f.getAgentFn(ctx, agentName, version)
	}
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) GetAgentVersions(context.Context, string) ([]*models.AgentResponse, error) {
	return nil, nil
}

func (f *fakeLocalPlatformRuntimeRegistry) PublishAgent(context.Context, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeLocalPlatformRuntimeRegistry) DeleteAgent(context.Context, string, string) error {
	return nil
}

func (f *fakeLocalPlatformRuntimeRegistry) SetAgentEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (f *fakeLocalPlatformRuntimeRegistry) GetAgentEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeLocalPlatformRuntimeRegistry) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	if f.resolveSkillsFn != nil {
		return f.resolveSkillsFn(ctx, manifest)
	}
	return nil, nil
}

func (f *fakeLocalPlatformRuntimeRegistry) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	if f.resolvePromptsFn != nil {
		return f.resolvePromptsFn(ctx, manifest)
	}
	if manifest == nil || len(manifest.Prompts) == 0 {
		return nil, nil
	}

	resolved := make([]platformtypes.ResolvedPrompt, 0, len(manifest.Prompts))
	for _, ref := range manifest.Prompts {
		promptName := ref.RegistryPromptName
		if promptName == "" {
			return nil, database.ErrInvalidInput
		}

		version := ref.RegistryPromptVersion
		var promptResp *models.PromptResponse
		var err error
		if version == "" || version == "latest" {
			promptResp, err = f.getPromptByNameFn(ctx, promptName)
		} else if f.getPromptByVerFn != nil {
			promptResp, err = f.getPromptByVerFn(ctx, promptName, version)
		} else {
			promptResp, err = f.getPromptByNameFn(ctx, promptName)
		}
		if err != nil {
			return nil, err
		}

		displayName := ref.Name
		if displayName == "" {
			displayName = promptName
		}
		resolved = append(resolved, platformtypes.ResolvedPrompt{
			Name:    displayName,
			Content: promptResp.Prompt.Content,
		})
	}

	return resolved, nil
}

func newLocalRuntimeServices(registry *fakeLocalPlatformRuntimeRegistry) (serversvc.Registry, agentsvc.Registry) {
	return registry, registry
}

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
			Scheme: "https",
			Host:   "example.com",
			Port:   443,
			Path:   "/mcp",
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

	registry := &fakeLocalPlatformRuntimeRegistry{}
	registry.getAgentFn = func(context.Context, string, string) (*models.AgentResponse, error) {
		return nil, database.ErrNotFound
	}
	serverService, agentService := newLocalRuntimeServices(registry)

	adapter := NewLocalDeploymentAdapter(serverService, agentService, tempDir, 8080)

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

	registry := &fakeLocalPlatformRuntimeRegistry{}
	registry.getAgentFn = func(context.Context, string, string) (*models.AgentResponse, error) {
		return nil, database.ErrNotFound
	}
	serverService, agentService := newLocalRuntimeServices(registry)

	adapter := NewLocalDeploymentAdapter(serverService, agentService, tempDir, 8080)

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

	registry := &fakeLocalPlatformRuntimeRegistry{}
	registry.getAgentFn = func(_ context.Context, name, version string) (*models.AgentResponse, error) {
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:  name,
					Image: "agent-image:latest",
					Prompts: []models.PromptRef{{
						RegistryPromptName: "system-prompt",
					}},
				},
				Version: version,
			},
		}, nil
	}
	registry.getPromptByNameFn = func(_ context.Context, promptName string) (*models.PromptResponse, error) {
		return &models.PromptResponse{Prompt: models.PromptJSON{Name: promptName, Content: "You are a helpful assistant.", Version: "latest"}}, nil
	}
	serverService, agentService := newLocalRuntimeServices(registry)

	adapter := NewLocalDeploymentAdapter(serverService, agentService, tempDir, 8080)

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
