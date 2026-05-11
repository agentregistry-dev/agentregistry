package local

import (
	"context"
	"testing"

	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
	runtimeutils "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/utils"
)

func TestBuildLocalRuntimeConfig_UsesDefaultAgentPortInGatewayRoute(t *testing.T) {
	cfg, err := BuildLocalRuntimeConfig(context.Background(), "/tmp/test-platform", 8081, "test-project", &runtimetypes.DesiredState{
		Agents: []*runtimetypes.Agent{{
			Name:       "demo-agent",
			Tag:        "1.0.0",
			Deployment: runtimetypes.AgentDeployment{Image: "demo-agent:latest"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildLocalRuntimeConfig() unexpected error: %v", err)
	}
	if cfg == nil || cfg.AgentGateway == nil {
		t.Fatal("expected agent gateway config")
	}
	if len(cfg.AgentGateway.Binds) == 0 || len(cfg.AgentGateway.Binds[0].Listeners) == 0 {
		t.Fatal("expected agent gateway listener")
	}

	routes := cfg.AgentGateway.Binds[0].Listeners[0].Routes
	if len(routes) != 1 {
		t.Fatalf("expected 1 route, got %d", len(routes))
	}
	if len(routes[0].Backends) != 1 {
		t.Fatalf("expected 1 backend, got %d", len(routes[0].Backends))
	}
	if got := routes[0].Backends[0].Host; got != "demo-agent:8080" {
		t.Fatalf("backend host = %q, want %q", got, "demo-agent:8080")
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
