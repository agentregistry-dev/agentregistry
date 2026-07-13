package local

import (
	"context"
	"slices"
	"testing"

	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
	runtimeutils "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/gateway"
)

func TestBuildLocalRuntimeConfig_UsesDefaultAgentPortInGatewayRoute(t *testing.T) {
	cfg, err := BuildLocalRuntimeConfig(context.Background(), "/tmp/test-runtime", 8081, "test-project", &runtimetypes.DesiredState{
		Agents: []*runtimetypes.Agent{{
			Name:       "demo-agent",
			Tag:        "1.0.0",
			Deployment: runtimetypes.AgentDeployment{Image: "demo-agent:latest"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildLocalRuntimeConfig() unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected local runtime config")
	}
	if len(cfg.GatewayConfig.Backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(cfg.GatewayConfig.Backends))
	}
	if got := cfg.GatewayConfig.Backends[0].URL; got != "demo-agent:8080" {
		t.Fatalf("backend URL = %q, want %q", got, "demo-agent:8080")
	}
}

func TestBuildLocalRuntimeConfig_MixedMCPAndAgentRoutesPresent(t *testing.T) {
	cfg, err := BuildLocalRuntimeConfig(context.Background(), "/tmp/test-runtime", 8081, "test-project", &runtimetypes.DesiredState{
		MCPServers: []*runtimetypes.MCPServer{{
			Name:          "demo-server",
			MCPServerType: runtimetypes.MCPServerTypeRemote,
			Remote: &runtimetypes.RemoteMCPTarget{
				Scheme: "https",
				Host:   "example.com",
				Port:   443,
				Path:   "/mcp",
			},
		}},
		Agents: []*runtimetypes.Agent{{
			Name:       "aaa-agent",
			Deployment: runtimetypes.AgentDeployment{Image: "aaa-agent:latest"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildLocalRuntimeConfig() unexpected error: %v", err)
	}

	// BuildLocalRuntimeConfig produces the generic desired config; deterministic
	// ordering of routes is the engine's job at render time (see the agentgateway
	// Render tests). Here we only assert both routes are present.
	routeNames := make([]string, 0, len(cfg.GatewayConfig.Routes))
	for _, r := range cfg.GatewayConfig.Routes {
		routeNames = append(routeNames, r.Name)
	}
	slices.Sort(routeNames)
	want := []string{"aaa-agent_route", gateway.MCPRouteName}
	if !slices.Equal(routeNames, want) {
		t.Fatalf("route names = %v, want %v", routeNames, want)
	}
}

func TestDefaultAgentPort(t *testing.T) {
	if got := defaultAgentPort(nil); got != runtimeutils.DefaultLocalAgentPort {
		t.Fatalf("defaultAgentPort(nil) = %d, want %d", got, runtimeutils.DefaultLocalAgentPort)
	}
	if got := defaultAgentPort(&runtimetypes.Agent{}); got != runtimeutils.DefaultLocalAgentPort {
		t.Fatalf("defaultAgentPort(zero) = %d, want %d", got, runtimeutils.DefaultLocalAgentPort)
	}
	if got := defaultAgentPort(&runtimetypes.Agent{Deployment: runtimetypes.AgentDeployment{Port: 9090}}); got != 9090 {
		t.Fatalf("defaultAgentPort(custom) = %d, want 9090", got)
	}
}
