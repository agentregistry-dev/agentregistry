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
	t.Cleanup(func() {
		runLocalComposeUp = originalComposeUp
		refreshLocalAgentMCPConfig = originalRefresh
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

	if err := adapter.Undeploy(context.Background(), deployment); err != nil {
		t.Fatalf("Undeploy() error = %v", err)
	}
	if !refreshCalled {
		t.Fatal("expected RefreshMCPConfig cleanup to be called for missing agent undeploy")
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
