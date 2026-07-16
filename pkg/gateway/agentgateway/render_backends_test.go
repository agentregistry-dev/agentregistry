package agentgateway

import (
	"context"
	"reflect"
	"testing"

	"go.yaml.in/yaml/v3"

	"github.com/agentregistry-dev/agentregistry/pkg/gateway"
)

// TestRender_ListenerJWTAuthAndTransformation covers the generic JWT auth and
// request-transformation listener policies, both of which map onto native
// listener FilterOrPolicy fields.
func TestRender_ListenerJWTAuthAndTransformation(t *testing.T) {
	desired := gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{
			Name:     "default",
			Protocol: "HTTP",
			Port:     3000,
			Policies: []gateway.PolicyRef{{Name: "listener"}},
		}},
		Policies: []gateway.Policy{{
			Name: "listener",
			Spec: gateway.PolicySpec{
				JWTAuth: &gateway.JWTAuthPolicy{
					Mode: "strict",
					Providers: []gateway.JWTProvider{{
						Issuer:    "https://issuer.example.com",
						Audiences: []string{"gateway"},
						JWKS:      gateway.JWKSSource{URL: "https://issuer.example.com/.well-known/jwks.json"},
					}},
				},
				Transformation: &gateway.TransformationPolicy{
					RequestMetadata: map[string]string{"role": "(jwt.groups) + []"},
				},
			},
		}},
	}

	want := &AgentGatewayConfig{
		Config: struct{}{},
		Binds: []LocalBind{{
			Port: 3000,
			Listeners: []LocalListener{{
				Name:        "default",
				GatewayName: "agentgateway",
				Protocol:    LocalListenerProtocolHTTP,
				Policies: &FilterOrPolicy{
					JWTAuth: &ListenerJWTAuth{
						Mode: "strict",
						Providers: []JWTProvider{{
							Issuer:    "https://issuer.example.com",
							Audiences: []string{"gateway"},
							JWKS:      JWKSSource{URL: "https://issuer.example.com/.well-known/jwks.json"},
						}},
					},
					Transformations: &TransformationPolicy{
						Request: &TransformStage{Metadata: map[string]string{"role": "(jwt.groups) + []"}},
					},
				},
			}},
		}},
	}

	got, err := Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Render() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

// TestRender_NamedBackendsAndRawPassthrough covers a route that references a
// named top-level MCP backend, an MCP target that routes through another named
// backend, and a provider-neutral Raw backend carried through verbatim. This is
// the shape a cloud-provider integration (e.g. AWS AgentCore) builds without
// leaking provider types into this package.
func TestRender_NamedBackendsAndRawPassthrough(t *testing.T) {
	desired := gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{
			Name:     "default",
			Protocol: "HTTP",
			Port:     3000,
		}},
		Routes: []gateway.Route{{
			Name:        "weather",
			Hostnames:   []string{"gw.example.com"},
			PathPrefix:  "/runtimes/weather/invocations",
			BackendRefs: []gateway.BackendRef{{Name: "weather"}},
			Policies:    []gateway.PolicyRef{{Name: "weather-authz"}},
		}},
		Backends: []gateway.Backend{
			{
				Name: "weather",
				MCP: &gateway.MCPBackend{Targets: []gateway.MCPTarget{{
					Name: "default",
					MCP:  &gateway.MCPTargetSpec{Backend: "weather-aws", Path: "/mcp"},
				}}},
			},
			{
				Name: "weather-aws",
				Extensions: []gateway.Extension{{
					Type: "aws",
					Spec: map[string]any{"agentCore": map[string]any{
						"agentRuntimeArn": "arn:aws:bedrock-agentcore:us-west-2:1234:runtime/weather",
						"qualifier":       "DEFAULT",
					}},
				}},
			},
		},
		Policies: []gateway.Policy{{
			Name: "weather-authz",
			Spec: gateway.PolicySpec{
				MCPAuthorization: &gateway.AuthzPolicy{
					Rules: []string{"true"},
				},
			},
		}},
	}

	want := &AgentGatewayConfig{
		Config: struct{}{},
		Binds: []LocalBind{{
			Port: 3000,
			Listeners: []LocalListener{{
				Name:        "default",
				GatewayName: "agentgateway",
				Protocol:    LocalListenerProtocolHTTP,
				Routes: []LocalRoute{{
					RouteName: "weather",
					Hostnames: []string{"gw.example.com"},
					Matches: []RouteMatch{{
						Path: PathMatch{PathPrefix: "/runtimes/weather/invocations"},
					}},
					Policies: &FilterOrPolicy{
						MCPAuthorization: &MCPAuthorization{
							Rules: []string{"true"},
						},
					},
					Backends: []RouteBackend{{Weight: 100, Backend: "/weather"}},
				}},
			}},
		}},
		Backends: []LocalBackend{
			{
				Name: "weather",
				MCP: &MCPBackend{Targets: []MCPTarget{{
					Name: "default",
					MCP:  &MCPTargetSpec{Backend: "/weather-aws", Path: "/mcp"},
				}}},
			},
			{
				Name: "weather-aws",
				Extra: map[string]any{"aws": map[string]any{
					"agentCore": map[string]any{
						"agentRuntimeArn": "arn:aws:bedrock-agentcore:us-west-2:1234:runtime/weather",
						"qualifier":       "DEFAULT",
					},
				}},
			},
		},
	}

	got, err := Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Render() mismatch\n got: %#v\nwant: %#v", got, want)
	}

	out, err := yaml.Marshal(got)
	if err != nil {
		t.Fatalf("yaml.Marshal: %v", err)
	}

	var decoded map[string]any
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	backends, ok := decoded["backends"].([]any)
	if !ok || len(backends) != 2 {
		t.Fatalf("expected 2 top-level backends, got %#v", decoded["backends"])
	}
	backend := backends[1].(map[string]any)
	if backend["name"] != "weather-aws" {
		t.Errorf("backend name = %v, want weather-aws", backend["name"])
	}
	aws, ok := backend["aws"].(map[string]any)
	if !ok {
		t.Fatalf("expected inlined aws key on backend, got %#v", backend)
	}
	agentCore, ok := aws["agentCore"].(map[string]any)
	if !ok {
		t.Fatalf("expected agentCore under aws, got %#v", aws)
	}
	if agentCore["agentRuntimeArn"] != "arn:aws:bedrock-agentcore:us-west-2:1234:runtime/weather" {
		t.Errorf("agentRuntimeArn = %v", agentCore["agentRuntimeArn"])
	}
	if agentCore["qualifier"] != "DEFAULT" {
		t.Errorf("qualifier = %v", agentCore["qualifier"])
	}
}

// TestRender_CORSAndAuthzRuleSet covers a route policy carrying both CORS and
// MCP authorization, asserting the authorization renders as agentgateway's
// flat RuleSet (a list of CEL strings), not a nested object.
func TestRender_CORSAndAuthzRuleSet(t *testing.T) {
	desired := gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{Name: "default", Protocol: "HTTP", Port: 3000}},
		Routes: []gateway.Route{{
			Name:       "svc",
			PathPrefix: "/mcp",
			MCP: &gateway.MCPBackend{Targets: []gateway.MCPTarget{{
				Name: "mcp",
				MCP:  &gateway.MCPTargetSpec{Host: "http://svc:8080/mcp"},
			}}},
			Policies: []gateway.PolicyRef{{Name: "svc-authz"}, {Name: "svc-cors"}},
		}},
		Policies: []gateway.Policy{
			{
				Name: "svc-authz",
				Spec: gateway.PolicySpec{
					MCPAuthorization: &gateway.AuthzPolicy{Rules: []string{"false"}},
				},
			},
			{
				Name: "svc-cors",
				Spec: gateway.PolicySpec{
					CORS: &gateway.CORSPolicy{
						AllowOrigins:  []string{"*"},
						AllowHeaders:  []string{"*"},
						ExposeHeaders: []string{"Mcp-Session-Id"},
					},
				},
			},
		},
	}

	got, err := Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	routePolicies := got.Binds[0].Listeners[0].Routes[0].Policies
	want := &FilterOrPolicy{
		CORS: &CORS{
			AllowOrigins:  []string{"*"},
			AllowHeaders:  []string{"*"},
			ExposeHeaders: []string{"Mcp-Session-Id"},
		},
		MCPAuthorization: &MCPAuthorization{Rules: []string{"false"}},
	}
	if !reflect.DeepEqual(routePolicies, want) {
		t.Errorf("route policies mismatch\n got: %#v\nwant: %#v", routePolicies, want)
	}
}

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
				Extensions: []gateway.Extension{{
					Type: "aws",
					Spec: map[string]any{"agentCore": map[string]any{"agentRuntimeArn": "arn:" + name}},
				}},
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
