package local

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

const applierTestPort = 21212

func mcpRenderedConfig(targets ...runtimetypes.MCPTarget) *runtimetypes.AgentGatewayConfig {
	return &runtimetypes.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []runtimetypes.LocalBind{{
			Port: applierTestPort,
			Listeners: []runtimetypes.LocalListener{{
				Name:     "default",
				Protocol: runtimetypes.LocalListenerProtocolHTTP,
				Routes: []runtimetypes.LocalRoute{{
					RouteName: localMCPRouteName,
					Matches: []runtimetypes.RouteMatch{{
						Path: runtimetypes.PathMatch{PathPrefix: "/mcp"},
					}},
					Backends: []runtimetypes.RouteBackend{{
						Weight: 100,
						MCP:    &runtimetypes.MCPBackend{Targets: targets},
					}},
				}},
			}},
		}},
	}
}

func loadMCPTargetNames(t *testing.T, runtimeDir string) []string {
	t.Helper()
	cfg, err := LoadLocalAgentGatewayConfig(runtimeDir, applierTestPort)
	if err != nil {
		t.Fatalf("LoadLocalAgentGatewayConfig: %v", err)
	}
	targets := extractMCPRouteTargets(cfg)
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}

func TestLocalApplier_ApplyWritesNewTargetsAndRoutes(t *testing.T) {
	dir := t.TempDir()
	applier := NewLocalApplier(dir, applierTestPort)

	rendered := mcpRenderedConfig(runtimetypes.MCPTarget{Name: "dep-1_weather", MCP: &runtimetypes.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-1"}, rendered); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 1 || names[0] != "dep-1_weather" {
		t.Fatalf("target names = %v, want [dep-1_weather]", names)
	}
}

func TestLocalApplier_ApplyUpsertsExistingTargetByName(t *testing.T) {
	dir := t.TempDir()
	applier := NewLocalApplier(dir, applierTestPort)

	first := mcpRenderedConfig(runtimetypes.MCPTarget{Name: "dep-1_weather", MCP: &runtimetypes.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-1"}, first); err != nil {
		t.Fatalf("Apply(first): %v", err)
	}

	second := mcpRenderedConfig(runtimetypes.MCPTarget{Name: "dep-1_weather", MCP: &runtimetypes.MCPTargetSpec{Host: "http://weather:9090/mcp"}})
	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-1"}, second); err != nil {
		t.Fatalf("Apply(second): %v", err)
	}

	cfg, err := LoadLocalAgentGatewayConfig(dir, applierTestPort)
	if err != nil {
		t.Fatalf("LoadLocalAgentGatewayConfig: %v", err)
	}
	targets := extractMCPRouteTargets(cfg)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target after upsert, got %d: %+v", len(targets), targets)
	}
	if got := targets[0].MCP.Host; got != "http://weather:9090/mcp" {
		t.Fatalf("target host = %q, want updated host", got)
	}
}

func TestLocalApplier_ApplyPreservesEntriesFromOtherDeployments(t *testing.T) {
	dir := t.TempDir()
	applier := NewLocalApplier(dir, applierTestPort)

	depA := mcpRenderedConfig(runtimetypes.MCPTarget{Name: "dep-a_weather", MCP: &runtimetypes.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-a"}, depA); err != nil {
		t.Fatalf("Apply(dep-a): %v", err)
	}
	depB := mcpRenderedConfig(runtimetypes.MCPTarget{Name: "dep-b_search", MCP: &runtimetypes.MCPTargetSpec{Host: "http://search:8080/mcp"}})
	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-b"}, depB); err != nil {
		t.Fatalf("Apply(dep-b): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 2 {
		t.Fatalf("expected both deployments' targets present, got %v", names)
	}
}

func TestLocalApplier_RemoveStripsOnlyMatchingDeploymentID(t *testing.T) {
	dir := t.TempDir()
	applier := NewLocalApplier(dir, applierTestPort)

	depA := mcpRenderedConfig(runtimetypes.MCPTarget{Name: "dep-a_weather", MCP: &runtimetypes.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-a"}, depA); err != nil {
		t.Fatalf("Apply(dep-a): %v", err)
	}
	depB := mcpRenderedConfig(runtimetypes.MCPTarget{Name: "dep-b_search", MCP: &runtimetypes.MCPTargetSpec{Host: "http://search:8080/mcp"}})
	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-b"}, depB); err != nil {
		t.Fatalf("Apply(dep-b): %v", err)
	}

	if err := applier.Remove(context.Background(), gateway.Target{Name: "dep-a"}); err != nil {
		t.Fatalf("Remove(dep-a): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 1 || names[0] != "dep-b_search" {
		t.Fatalf("target names after remove = %v, want [dep-b_search]", names)
	}
}

func TestLocalApplier_RemoveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	applier := NewLocalApplier(dir, applierTestPort)

	if err := applier.Remove(context.Background(), gateway.Target{Name: "never-applied"}); err != nil {
		t.Fatalf("Remove(never-applied) first call: %v", err)
	}
	if err := applier.Remove(context.Background(), gateway.Target{Name: "never-applied"}); err != nil {
		t.Fatalf("Remove(never-applied) second call: %v", err)
	}
}

func TestLocalApplier_RemoveRejectsEmptyTargetName(t *testing.T) {
	dir := t.TempDir()
	applier := NewLocalApplier(dir, applierTestPort)

	if err := applier.Remove(context.Background(), gateway.Target{Name: "  "}); err == nil {
		t.Fatal("expected error for empty target name, got nil")
	}
}

func TestLocalApplier_ApplyNilRenderedIsNoop(t *testing.T) {
	dir := t.TempDir()
	applier := NewLocalApplier(dir, applierTestPort)

	if err := applier.Apply(context.Background(), gateway.Target{Name: "dep-1"}, nil); err != nil {
		t.Fatalf("Apply(nil): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 0 {
		t.Fatalf("expected no targets written, got %v", names)
	}
}
