package local

import (
	"context"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	platformutils "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
)

func TestBuildLocalPlatformConfig_UsesDefaultAgentPortInGatewayRoute(t *testing.T) {
	cfg, err := BuildLocalPlatformConfig(context.Background(), "/tmp/test-platform", 8081, "test-project", &platformtypes.DesiredState{
		Agents: []*platformtypes.Agent{{
			Name:       "demo-agent",
			Version:    "1.0.0",
			Deployment: platformtypes.AgentDeployment{Image: "demo-agent:latest"},
		}},
	})
	if err != nil {
		t.Fatalf("BuildLocalPlatformConfig() unexpected error: %v", err)
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

func TestBuildRemoteMCPURL(t *testing.T) {
	tests := []struct {
		name   string
		remote *platformtypes.RemoteMCPServer
		want   string
	}{
		{"https standard port", &platformtypes.RemoteMCPServer{Host: "example.com", Port: 443, Path: "/mcp"}, "https://example.com/mcp"},
		{"https custom port", &platformtypes.RemoteMCPServer{Host: "example.com", Port: 8443, Path: "/mcp"}, "https://example.com:8443/mcp"},
		{"http standard port", &platformtypes.RemoteMCPServer{Host: "example.com", Port: 80, Path: "/sse"}, "http://example.com/sse"},
		{"http custom port", &platformtypes.RemoteMCPServer{Host: "localhost", Port: 3005, Path: "/mcp/"}, "https://localhost:3005/mcp/"},
		{"empty path", &platformtypes.RemoteMCPServer{Host: "example.com", Port: 443, Path: ""}, "https://example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildRemoteMCPURL(tt.remote); got != tt.want {
				t.Errorf("buildRemoteMCPURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDefaultAgentPort(t *testing.T) {
	if got := defaultAgentPort(nil); got != platformutils.DefaultLocalAgentPort {
		t.Fatalf("defaultAgentPort(nil) = %d, want %d", got, platformutils.DefaultLocalAgentPort)
	}
	if got := defaultAgentPort(&platformtypes.Agent{}); got != platformutils.DefaultLocalAgentPort {
		t.Fatalf("defaultAgentPort(zero) = %d, want %d", got, platformutils.DefaultLocalAgentPort)
	}
	if got := defaultAgentPort(&platformtypes.Agent{Deployment: platformtypes.AgentDeployment{Port: 9090}}); got != 9090 {
		t.Fatalf("defaultAgentPort(custom) = %d, want 9090", got)
	}
}
