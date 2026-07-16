package agentgateway

import (
	"context"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/agentregistry-dev/agentregistry/pkg/gateway"
)

func TestRenderYAML_EgressListeners(t *testing.T) {
	rendered := renderYAMLMap(t, gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{
			Name:     "connect",
			Protocol: "HTTPS",
			Port:     8443,
			TLS: &gateway.TLSConfig{
				Mode: "Terminate",
				CertificateRefs: []gateway.ObjectRef{{
					Kind: "Secret",
					Name: "egress-srv",
				}},
			},
			AllowedRoutes: &gateway.AllowedRoutes{
				Namespaces: &gateway.AllowedRouteNamespaces{From: "Same"},
			},
			Policies: gateway.PolicySpec{
				FrontendConnect: &gateway.FrontendConnectPolicy{Enabled: true},
			},
		}, {
			Name:     "https",
			Protocol: "HTTPS",
			Port:     443,
			TLS: &gateway.TLSConfig{
				Mode: "Terminate",
				Options: map[string]string{
					"agentgateway.dev/tls-certificate-source": "DYNAMIC_CA",
				},
				CertificateRefs: []gateway.ObjectRef{{
					Kind: "Secret",
					Name: "egress-ca",
				}},
			},
			AllowedRoutes: &gateway.AllowedRoutes{
				Namespaces: &gateway.AllowedRouteNamespaces{From: "Same"},
			},
		}},
	})

	binds := rendered["binds"].([]any)
	assertEqual(t, binds[0].(map[string]any)["port"], 443, "first bind port")
	httpsListener := binds[0].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	assertEqual(t, httpsListener["name"], "https", "https listener name")
	tls := httpsListener["tls"].(map[string]any)
	assertEqual(t, tls["mode"], "Terminate", "https tls mode")
	assertEqual(t, tls["options"].(map[string]any)["agentgateway.dev/tls-certificate-source"], "DYNAMIC_CA", "dynamic ca option")
	assertEqual(t, tls["certificateRefs"].([]any)[0].(map[string]any)["name"], "egress-ca", "ca cert ref")
	assertEqual(t, httpsListener["allowedRoutes"].(map[string]any)["namespaces"].(map[string]any)["from"], "Same", "https allowed namespace")

	assertEqual(t, binds[1].(map[string]any)["port"], 8443, "second bind port")
	connectListener := binds[1].(map[string]any)["listeners"].([]any)[0].(map[string]any)
	assertEqual(t, connectListener["name"], "connect", "connect listener name")
	assertEqual(t, connectListener["policies"].(map[string]any)["frontendConnect"].(map[string]any)["enabled"], true, "connect policy")
}

func TestRenderYAML_RoutesBackendsAndPolicies(t *testing.T) {
	rendered := renderYAMLMap(t, gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{
			Name:     "http",
			Protocol: "HTTP",
			Port:     8080,
		}},
		Routes: []gateway.Route{
			{
				Name:        "api",
				Hostnames:   []string{"example.com"},
				PathPrefix:  "/api",
				BackendRefs: []gateway.BackendRef{{Name: "svc"}},
				Policies: gateway.PolicySpec{
					CORS: &gateway.CORSPolicy{AllowOrigins: []string{"*"}},
				},
			},
			{
				Name:       "agent-route",
				PathPrefix: "/agents/foo",
				Policies: gateway.PolicySpec{
					A2A:        &gateway.A2APolicy{},
					URLRewrite: &gateway.URLRewritePolicy{PathPrefix: "/"},
				},
			},
		},
		Backends: []gateway.Backend{{
			Name: "svc",
			URL:  "http://svc:9000",
		}},
	})

	routes := rendered["binds"].([]any)[0].(map[string]any)["listeners"].([]any)[0].(map[string]any)["routes"].([]any)
	api := routes[0].(map[string]any)
	assertEqual(t, api["name"], "agent-route", "routes sort by name")
	assertEqual(t, api["policies"].(map[string]any)["urlRewrite"].(map[string]any)["path"].(map[string]any)["prefix"], "/", "url rewrite")
	if _, ok := api["policies"].(map[string]any)["a2a"]; !ok {
		t.Fatalf("a2a policy missing: %#v", api["policies"])
	}

	backendRoute := routes[1].(map[string]any)
	assertEqual(t, backendRoute["hostnames"].([]any)[0], "example.com", "hostname")
	assertEqual(t, backendRoute["backends"].([]any)[0].(map[string]any)["host"], "http://svc:9000", "backend host")
	assertEqual(t, backendRoute["policies"].(map[string]any)["cors"].(map[string]any)["allowOrigins"].([]any)[0], "*", "cors")
}

func TestRenderYAML_MCPTargetsAndExtensionBackends(t *testing.T) {
	rendered := renderYAMLMap(t, gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{
			Name:     "http",
			Protocol: "HTTP",
			Port:     8080,
		}},
		Routes: []gateway.Route{{
			Name:       gateway.MCPRouteName,
			PathPrefix: "/mcp",
			MCP: &gateway.MCPBackend{Targets: []gateway.MCPTarget{
				{Name: "z-sse", SSE: &gateway.SSETargetSpec{Scheme: "http", Host: "sse-host", Port: 9001, Path: "/sse"}},
				{Name: "a-stdio", Stdio: &gateway.StdioTargetSpec{Cmd: "server", Args: []string{"--flag"}}},
				{Name: "m-mcp", MCP: &gateway.MCPTargetSpec{Backend: "aws-backend", Path: "/mcp"}},
			}},
			Policies: gateway.PolicySpec{
				MCPAuthorization: &gateway.AuthzPolicy{Rules: []string{"true"}},
			},
		}},
		Backends: []gateway.Backend{{
			Name: "aws-backend",
			Extensions: map[string]any{
				"aws": map[string]any{"agentCore": map[string]any{"agentRuntimeArn": "arn:test"}},
			},
		}},
	})

	route := rendered["binds"].([]any)[0].(map[string]any)["listeners"].([]any)[0].(map[string]any)["routes"].([]any)[0].(map[string]any)
	targets := route["backends"].([]any)[0].(map[string]any)["mcp"].(map[string]any)["targets"].([]any)
	assertEqual(t, targets[0].(map[string]any)["name"], "a-stdio", "targets sort by name")
	assertEqual(t, targets[1].(map[string]any)["mcp"].(map[string]any)["backend"], "/aws-backend", "mcp backend ref")
	assertEqual(t, route["policies"].(map[string]any)["mcpAuthorization"].(map[string]any)["rules"].([]any)[0], "true", "mcp authz rule")
	backends := rendered["backends"].([]any)
	assertEqual(t, backends[0].(map[string]any)["aws"].(map[string]any)["agentCore"].(map[string]any)["agentRuntimeArn"], "arn:test", "extension backend")
}

func TestRenderYAML_IsDeterministic(t *testing.T) {
	desired := gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{
			{Name: "b", Protocol: "HTTP", Port: 9000},
			{Name: "a", Protocol: "HTTP", Port: 8000},
		},
		Routes: []gateway.Route{
			{Name: "z", PathPrefix: "/z"},
			{Name: "a", PathPrefix: "/a"},
		},
	}
	first, err := RenderYAML(context.Background(), desired)
	if err != nil {
		t.Fatalf("RenderYAML() unexpected error: %v", err)
	}
	second, err := RenderYAML(context.Background(), desired)
	if err != nil {
		t.Fatalf("RenderYAML() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("RenderYAML() is not deterministic across identical inputs")
	}
}

const engineTestPort = 21212

func renderYAMLMap(t *testing.T, desired gateway.Config) map[string]any {
	t.Helper()
	out, err := RenderYAML(context.Background(), desired)
	if err != nil {
		t.Fatalf("RenderYAML() unexpected error: %v", err)
	}
	var rendered map[string]any
	if err := yaml.Unmarshal(out, &rendered); err != nil {
		t.Fatalf("yaml.Unmarshal: %v\n%s", err, out)
	}
	return rendered
}

func assertEqual(t *testing.T, got, want any, label string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
}

func mcpDesiredConfig(targets ...gateway.MCPTarget) gateway.Config {
	return gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{
			Name:     "default",
			Protocol: "HTTP",
			Port:     engineTestPort,
		}},
		Routes: []gateway.Route{{
			Name:       gateway.MCPRouteName,
			PathPrefix: "/mcp",
			MCP:        &gateway.MCPBackend{Targets: targets},
		}},
	}
}

func loadMCPTargetNames(t *testing.T, dir string) []string {
	t.Helper()
	cfg, err := loadConfig(dir, engineTestPort)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	targets := extractMCPRouteTargets(cfg)
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	return names
}

func TestEngine_ApplyUpsertsAndRemoveTargets(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	depA := mcpDesiredConfig(gateway.MCPTarget{Name: "dep-a_weather", MCP: &gateway.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-a"}, depA); err != nil {
		t.Fatalf("Apply(dep-a): %v", err)
	}
	depA = mcpDesiredConfig(gateway.MCPTarget{Name: "dep-a_weather", MCP: &gateway.MCPTargetSpec{Host: "http://weather:9090/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-a"}, depA); err != nil {
		t.Fatalf("Apply(dep-a update): %v", err)
	}
	depB := mcpDesiredConfig(gateway.MCPTarget{Name: "dep-b_search", MCP: &gateway.MCPTargetSpec{Host: "http://search:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-b"}, depB); err != nil {
		t.Fatalf("Apply(dep-b): %v", err)
	}

	cfg, err := loadConfig(dir, engineTestPort)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	targets := extractMCPRouteTargets(cfg)
	if len(targets) != 2 {
		t.Fatalf("target count = %d, want 2: %+v", len(targets), targets)
	}
	if got := targets[0].MCP.Host; got != "http://weather:9090/mcp" {
		t.Fatalf("updated target host = %q, want http://weather:9090/mcp", got)
	}

	if err := engine.Remove(context.Background(), gateway.Target{Name: "dep-a"}); err != nil {
		t.Fatalf("Remove(dep-a): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 1 || names[0] != "dep-b_search" {
		t.Fatalf("target names after remove = %v, want [dep-b_search]", names)
	}
	if err := engine.Remove(context.Background(), gateway.Target{Name: "dep-a"}); err != nil {
		t.Fatalf("Remove(dep-a) idempotent: %v", err)
	}
}

func TestEngine_RemoveDoesNotStripDeploymentIDPrefixCollision(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	dep1 := mcpDesiredConfig(gateway.MCPTarget{Name: "weather-dep-1", MCP: &gateway.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-1"}, dep1); err != nil {
		t.Fatalf("Apply(dep-1): %v", err)
	}
	dep10 := mcpDesiredConfig(gateway.MCPTarget{Name: "search-dep-10", MCP: &gateway.MCPTargetSpec{Host: "http://search:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-10"}, dep10); err != nil {
		t.Fatalf("Apply(dep-10): %v", err)
	}

	if err := engine.Remove(context.Background(), gateway.Target{Name: "dep-1"}); err != nil {
		t.Fatalf("Remove(dep-1): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 1 || names[0] != "search-dep-10" {
		t.Fatalf("target names after remove = %v, want [search-dep-10]: removing dep-1 must not strip dep-10", names)
	}
}

func TestEngine_RemoveIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	if err := engine.Remove(context.Background(), gateway.Target{Name: "never-applied"}); err != nil {
		t.Fatalf("Remove(never-applied) first call: %v", err)
	}
	if err := engine.Remove(context.Background(), gateway.Target{Name: "never-applied"}); err != nil {
		t.Fatalf("Remove(never-applied) second call: %v", err)
	}
}

func TestEngine_RemoveRejectsEmptyTargetName(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	if err := engine.Remove(context.Background(), gateway.Target{Name: "  "}); err == nil {
		t.Fatal("expected error for empty target name, got nil")
	}
}
