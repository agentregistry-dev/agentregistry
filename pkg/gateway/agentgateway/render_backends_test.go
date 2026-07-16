package agentgateway

import (
	"context"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/gateway"
)

func awsBackendDesiredConfig(name string) gateway.Config {
	return gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{Name: "default", Protocol: "HTTP", Port: engineTestPort}},
		Routes: []gateway.Route{{
			Name:        name,
			PathPrefix:  "/runtimes/" + name + "/invocations",
			BackendRefs: []gateway.BackendRef{{Name: name}},
		}},
		Backends: []gateway.Backend{
			{
				Name: name,
				MCP: &gateway.MCPBackend{Targets: []gateway.MCPTarget{{
					Name: "default",
					MCP:  &gateway.MCPTargetSpec{Backend: name + "-aws", Path: "/mcp"},
				}}},
			},
			{
				Name: name + "-aws",
				Extensions: map[string]any{
					"aws": map[string]any{"agentCore": map[string]any{"agentRuntimeArn": "arn:" + name}},
				},
			},
		},
	}
}

func loadBackendNames(t *testing.T, dir string) []string {
	t.Helper()
	cfg, err := loadConfig(dir, engineTestPort)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	names := make([]string, 0, len(cfg.Backends))
	for _, b := range cfg.Backends {
		names = append(names, b.Name)
	}
	return names
}

func TestEngine_MergesPreservesAndRemovesTopLevelBackends(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-a"}, awsBackendDesiredConfig("dep-a")); err != nil {
		t.Fatalf("Apply(dep-a): %v", err)
	}
	mcpOnly := mcpDesiredConfig(gateway.MCPTarget{Name: "dep-b_weather", MCP: &gateway.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-b"}, mcpOnly); err != nil {
		t.Fatalf("Apply(dep-b): %v", err)
	}
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-c"}, awsBackendDesiredConfig("dep-c")); err != nil {
		t.Fatalf("Apply(dep-c): %v", err)
	}

	got := loadBackendNames(t, dir)
	want := []string{"dep-a", "dep-a-aws", "dep-c", "dep-c-aws"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backend names = %v, want %v", got, want)
	}

	if err := engine.Remove(context.Background(), gateway.Target{Name: "dep-a"}); err != nil {
		t.Fatalf("Remove(dep-a): %v", err)
	}
	got = loadBackendNames(t, dir)
	want = []string{"dep-c", "dep-c-aws"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("backend names after remove = %v, want %v", got, want)
	}
}
