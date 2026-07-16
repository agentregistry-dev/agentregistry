package agentgateway

import (
	"context"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/gateway"
)

func TestAgentGatewayEngine_Render(t *testing.T) {
	tests := []struct {
		name    string
		desired gateway.Config
		want    *AgentGatewayConfig
	}{
		{
			name:    "empty config renders no binds",
			desired: gateway.Config{},
			want:    &AgentGatewayConfig{Config: struct{}{}},
		},
		{
			name: "single http listener",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
				}},
			},
			want: &AgentGatewayConfig{
				Config: struct{}{},
				Binds: []LocalBind{{
					Port: 8080,
					Listeners: []LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    LocalListenerProtocolHTTP,
					}},
				}},
			},
		},
		{
			name: "route and backend rendering with default weight",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
				}},
				Routes: []gateway.Route{{
					Name:       "api",
					Hostnames:  []string{"example.com"},
					PathPrefix: "/api",
					BackendRefs: []gateway.BackendRef{{
						Name: "svc",
					}},
				}},
				Backends: []gateway.Backend{{
					Name: "svc",
					URL:  "http://svc:9000",
				}},
			},
			want: &AgentGatewayConfig{
				Config: struct{}{},
				Binds: []LocalBind{{
					Port: 8080,
					Listeners: []LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    LocalListenerProtocolHTTP,
						Routes: []LocalRoute{{
							RouteName: "api",
							Hostnames: []string{"example.com"},
							Matches: []RouteMatch{{
								Path: PathMatch{PathPrefix: "/api"},
							}},
							Backends: []RouteBackend{{
								Weight: 100,
								Host:   "http://svc:9000",
							}},
						}},
					}},
				}},
			},
		},
		{
			name: "mcp authorization policy",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					Policies: []gateway.PolicyRef{{Name: "mcp-authz"}},
				}},
				Policies: []gateway.Policy{{
					Name: "mcp-authz",
					Type: "MCPAuthorization",
					Spec: gateway.PolicySpec{
						MCPAuthorization: &gateway.AuthzPolicy{
							Rules: []string{"request.method == 'tools/call'"},
						},
					},
				}},
			},
			want: &AgentGatewayConfig{
				Config: struct{}{},
				Binds: []LocalBind{{
					Port: 8080,
					Listeners: []LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    LocalListenerProtocolHTTP,
						Policies: &FilterOrPolicy{
							MCPAuthorization: &MCPAuthorization{
								Rules: []string{"request.method == 'tools/call'"},
							},
						},
					}},
				}},
			},
		},
		{
			name: "listeners sorted by port then name",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{
					{Name: "b", Protocol: "HTTP", Port: 9000},
					{Name: "a", Protocol: "HTTP", Port: 9000},
					{Name: "c", Protocol: "HTTP", Port: 8000},
				},
			},
			want: &AgentGatewayConfig{
				Config: struct{}{},
				Binds: []LocalBind{
					{
						Port: 8000,
						Listeners: []LocalListener{{
							Name: "c", GatewayName: "agentgateway", Protocol: LocalListenerProtocolHTTP,
						}},
					},
					{
						Port: 9000,
						Listeners: []LocalListener{
							{Name: "a", GatewayName: "agentgateway", Protocol: LocalListenerProtocolHTTP},
							{Name: "b", GatewayName: "agentgateway", Protocol: LocalListenerProtocolHTTP},
						},
					},
				},
			},
		},
		{
			name: "mcp backend renders local target variants sorted by name",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
				}},
				Routes: []gateway.Route{{
					Name:       "mcp_route",
					PathPrefix: "/mcp",
					MCP: &gateway.MCPBackend{
						Targets: []gateway.MCPTarget{
							{
								Name: "z-sse",
								SSE: &gateway.SSETargetSpec{
									Scheme: "http",
									Host:   "sse-host",
									Port:   9001,
									Path:   "/sse",
								},
							},
							{
								Name: "a-stdio",
								Stdio: &gateway.StdioTargetSpec{
									Cmd:  "server",
									Args: []string{"--flag"},
									Env:  map[string]string{"KEY": "VALUE"},
								},
							},
							{
								Name: "m-mcp",
								MCP:  &gateway.MCPTargetSpec{Host: "mcp-host:9002"},
							},
						},
					},
				}},
			},
			want: &AgentGatewayConfig{
				Config: struct{}{},
				Binds: []LocalBind{{
					Port: 8080,
					Listeners: []LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    LocalListenerProtocolHTTP,
						Routes: []LocalRoute{{
							RouteName: "mcp_route",
							Matches: []RouteMatch{{
								Path: PathMatch{PathPrefix: "/mcp"},
							}},
							Backends: []RouteBackend{{
								Weight: 100,
								MCP: &MCPBackend{
									Targets: []MCPTarget{
										{
											Name: "a-stdio",
											Stdio: &StdioTargetSpec{
												Cmd:  "server",
												Args: []string{"--flag"},
												Env:  map[string]string{"KEY": "VALUE"},
											},
										},
										{
											Name: "m-mcp",
											MCP:  &MCPTargetSpec{Host: "mcp-host:9002"},
										},
										{
											Name: "z-sse",
											SSE: &SSETargetSpec{
												Scheme: "http",
												Host:   "sse-host",
												Port:   9001,
												Path:   "/sse",
											},
										},
									},
								},
							}},
						}},
					}},
				}},
			},
		},
		{
			name: "agent route policies",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
				}},
				Routes: []gateway.Route{{
					Name:       "agent-route",
					PathPrefix: "/agents/foo",
					Policies:   []gateway.PolicyRef{{Name: "agent-policy"}},
				}},
				Policies: []gateway.Policy{{
					Name: "agent-policy",
					Spec: gateway.PolicySpec{
						A2A:        &gateway.A2APolicy{},
						URLRewrite: &gateway.URLRewritePolicy{PathPrefix: "/"},
					},
				}},
			},
			want: &AgentGatewayConfig{
				Config: struct{}{},
				Binds: []LocalBind{{
					Port: 8080,
					Listeners: []LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    LocalListenerProtocolHTTP,
						Routes: []LocalRoute{{
							RouteName: "agent-route",
							Matches: []RouteMatch{{
								Path: PathMatch{PathPrefix: "/agents/foo"},
							}},
							Policies: &FilterOrPolicy{
								A2A:        &A2APolicy{},
								URLRewrite: &URLRewrite{Path: &PathRedirect{Prefix: "/"}},
							},
						}},
					}},
				}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := Render(context.Background(), tt.desired)
			if err != nil {
				t.Fatalf("Render() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(rendered, tt.want) {
				t.Errorf("Render() mismatch\n got: %#v\nwant: %#v", rendered, tt.want)
			}
		})
	}
}

func TestAgentGatewayEngine_RenderIsDeterministic(t *testing.T) {
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
	first, err := Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	second, err := Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Errorf("Render() is not deterministic across identical inputs")
	}
}

const engineTestPort = 21212

// mcpDesiredConfig builds a generic gateway.Config with the given MCP targets
// under the well-known MCP route. Apply renders it into native config
// internally, so these tests exercise Apply through the same path production
// uses rather than hand-building native config.
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
