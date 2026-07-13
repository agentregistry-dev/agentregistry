package agentgateway

import (
	"context"
	"reflect"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	types "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

func TestAgentGatewayEngine_Render(t *testing.T) {
	tests := []struct {
		name    string
		desired gateway.Config
		want    *types.AgentGatewayConfig
	}{
		{
			name:    "empty config renders no binds",
			desired: gateway.Config{},
			want:    &types.AgentGatewayConfig{Config: struct{}{}},
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
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
					}},
				}},
			},
		},
		{
			name: "listener tls config with certificate refs",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "https",
					Protocol: "HTTPS",
					Port:     8443,
					TLS: &gateway.TLSConfig{
						Mode: "Terminate",
						CertificateRefs: []gateway.ObjectRef{{
							Group:     "",
							Kind:      "Secret",
							Name:      "tls-cert",
							Namespace: "default",
						}},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8443,
					Listeners: []types.LocalListener{{
						Name:        "https",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTPS,
						TLS: &types.LocalTLSServerConfig{
							Mode: "Terminate",
							CertificateRefs: []types.LocalObjectReference{{
								Kind:      "Secret",
								Name:      "tls-cert",
								Namespace: "default",
							}},
						},
					}},
				}},
			},
		},
		{
			name: "listener tls options",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "https",
					Protocol: "HTTPS",
					Port:     8443,
					TLS: &gateway.TLSConfig{
						Mode:    "Terminate",
						Options: map[string]string{"minVersion": "1.3"},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8443,
					Listeners: []types.LocalListener{{
						Name:        "https",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTPS,
						TLS: &types.LocalTLSServerConfig{
							Mode:    "Terminate",
							Options: map[string]string{"minVersion": "1.3"},
						},
					}},
				}},
			},
		},
		{
			name: "listener allowed routes",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					AllowedRoutes: &gateway.AllowedRoutes{
						Namespaces: []string{"default"},
						Kinds:      []string{"HTTPRoute"},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						AllowedRoutes: &types.LocalAllowedRoutes{
							Namespaces: []string{"default"},
							Kinds:      []string{"HTTPRoute"},
						},
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
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Routes: []types.LocalRoute{{
							RouteName: "api",
							Hostnames: []string{"example.com"},
							Matches: []types.RouteMatch{{
								Path: types.PathMatch{PathPrefix: "/api"},
							}},
							Backends: []types.RouteBackend{{
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
							Action:           "ALLOW",
							MatchExpressions: []string{"request.method == 'tools/call'"},
						},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Policies: &types.FilterOrPolicy{
							MCPAuthorization: &types.MCPAuthorization{
								Rules: map[string]any{
									"action":           "ALLOW",
									"matchExpressions": []string{"request.method == 'tools/call'"},
								},
							},
						},
					}},
				}},
			},
		},
		{
			name: "traffic authorization policy",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					Policies: []gateway.PolicyRef{{Name: "traffic-authz"}},
				}},
				Policies: []gateway.Policy{{
					Name: "traffic-authz",
					Type: "TrafficAuthorization",
					Spec: gateway.PolicySpec{
						TrafficAuthorization: &gateway.AuthzPolicy{
							Action:           "DENY",
							MatchExpressions: []string{"source.namespace != 'trusted'"},
						},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Policies: &types.FilterOrPolicy{
							TrafficAuthorization: &types.TrafficAuthorization{
								Rules: map[string]any{
									"action":           "DENY",
									"matchExpressions": []string{"source.namespace != 'trusted'"},
								},
							},
						},
					}},
				}},
			},
		},
		{
			name: "frontend connect policy with authorization",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					Policies: []gateway.PolicyRef{{Name: "connect"}},
				}},
				Policies: []gateway.Policy{{
					Name: "connect",
					Type: "FrontendConnect",
					Spec: gateway.PolicySpec{
						FrontendConnect: &gateway.FrontendConnectPolicy{
							Enabled: true,
							Authorization: &gateway.AuthzPolicy{
								Action:           "ALLOW",
								MatchExpressions: []string{"connect.host == 'upstream:443'"},
							},
						},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Policies: &types.FilterOrPolicy{
							FrontendConnect: &types.FrontendConnect{
								Enabled: true,
								Rules: map[string]any{
									"action":           "ALLOW",
									"matchExpressions": []string{"connect.host == 'upstream:443'"},
								},
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
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{
					{
						Port: 8000,
						Listeners: []types.LocalListener{{
							Name: "c", GatewayName: "agentgateway", Protocol: types.LocalListenerProtocolHTTP,
						}},
					},
					{
						Port: 9000,
						Listeners: []types.LocalListener{
							{Name: "a", GatewayName: "agentgateway", Protocol: types.LocalListenerProtocolHTTP},
							{Name: "b", GatewayName: "agentgateway", Protocol: types.LocalListenerProtocolHTTP},
						},
					},
				},
			},
		},
		{
			name: "mcp backend renders all target spec variants sorted by name",
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
							{
								Name: "o-openapi",
								OpenAPI: &gateway.OpenAPITargetSpec{
									Host:   "openapi-host",
									Port:   9003,
									Schema: map[string]any{"openapi": "3.0.0"},
								},
							},
						},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Routes: []types.LocalRoute{{
							RouteName: "mcp_route",
							Matches: []types.RouteMatch{{
								Path: types.PathMatch{PathPrefix: "/mcp"},
							}},
							Backends: []types.RouteBackend{{
								Weight: 100,
								MCP: &types.MCPBackend{
									Targets: []types.MCPTarget{
										{
											Name: "a-stdio",
											Stdio: &types.StdioTargetSpec{
												Cmd:  "server",
												Args: []string{"--flag"},
												Env:  map[string]string{"KEY": "VALUE"},
											},
										},
										{
											Name: "m-mcp",
											MCP:  &types.MCPTargetSpec{Host: "mcp-host:9002"},
										},
										{
											Name: "o-openapi",
											OpenAPI: &types.OpenAPITargetSpec{
												Host:   "openapi-host",
												Port:   9003,
												Schema: map[string]any{"openapi": "3.0.0"},
											},
										},
										{
											Name: "z-sse",
											SSE: &types.SSETargetSpec{
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
			name: "mcp backend takes precedence over backend refs when both set",
			desired: gateway.Config{
				ClassName: "agentgateway",
				Listeners: []gateway.Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
				}},
				Routes: []gateway.Route{{
					Name:       "mixed",
					PathPrefix: "/mixed",
					BackendRefs: []gateway.BackendRef{{
						Name: "svc",
					}},
					MCP: &gateway.MCPBackend{
						Targets: []gateway.MCPTarget{{
							Name: "target",
							MCP:  &gateway.MCPTargetSpec{Host: "mcp-host"},
						}},
					},
				}},
				Backends: []gateway.Backend{{
					Name: "svc",
					URL:  "http://svc:9000",
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Routes: []types.LocalRoute{{
							RouteName: "mixed",
							Matches: []types.RouteMatch{{
								Path: types.PathMatch{PathPrefix: "/mixed"},
							}},
							Backends: []types.RouteBackend{{
								Weight: 100,
								MCP: &types.MCPBackend{
									Targets: []types.MCPTarget{{
										Name: "target",
										MCP:  &types.MCPTargetSpec{Host: "mcp-host"},
									}},
								},
							}},
						}},
					}},
				}},
			},
		},
		{
			name: "a2a policy",
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
					Policies:   []gateway.PolicyRef{{Name: "a2a"}},
				}},
				Policies: []gateway.Policy{{
					Name: "a2a",
					Type: "A2A",
					Spec: gateway.PolicySpec{
						A2A: &gateway.A2APolicy{},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Routes: []types.LocalRoute{{
							RouteName: "agent-route",
							Matches: []types.RouteMatch{{
								Path: types.PathMatch{PathPrefix: "/agents/foo"},
							}},
							Policies: &types.FilterOrPolicy{
								A2A: &types.A2APolicy{},
							},
						}},
					}},
				}},
			},
		},
		{
			name: "url rewrite policy",
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
					Policies:   []gateway.PolicyRef{{Name: "rewrite"}},
				}},
				Policies: []gateway.Policy{{
					Name: "rewrite",
					Type: "URLRewrite",
					Spec: gateway.PolicySpec{
						URLRewrite: &gateway.URLRewritePolicy{PathPrefix: "/"},
					},
				}},
			},
			want: &types.AgentGatewayConfig{
				Config: struct{}{},
				Binds: []types.LocalBind{{
					Port: 8080,
					Listeners: []types.LocalListener{{
						Name:        "http",
						GatewayName: "agentgateway",
						Protocol:    types.LocalListenerProtocolHTTP,
						Routes: []types.LocalRoute{{
							RouteName: "agent-route",
							Matches: []types.RouteMatch{{
								Path: types.PathMatch{PathPrefix: "/agents/foo"},
							}},
							Policies: &types.FilterOrPolicy{
								URLRewrite: &types.URLRewrite{Path: &types.PathRedirect{Prefix: "/"}},
							},
						}},
					}},
				}},
			},
		},
	}

	engine := NewEngine("", 0)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := engine.Render(context.Background(), tt.desired)
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
	engine := NewEngine("", 0)

	first, err := engine.Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	second, err := engine.Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Errorf("Render() is not deterministic across identical inputs")
	}
}

const engineTestPort = 21212

func mcpRenderedConfig(targets ...types.MCPTarget) *types.AgentGatewayConfig {
	return &types.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []types.LocalBind{{
			Port: engineTestPort,
			Listeners: []types.LocalListener{{
				Name:     "default",
				Protocol: types.LocalListenerProtocolHTTP,
				Routes: []types.LocalRoute{{
					RouteName: gateway.MCPRouteName,
					Matches: []types.RouteMatch{{
						Path: types.PathMatch{PathPrefix: "/mcp"},
					}},
					Backends: []types.RouteBackend{{
						Weight: 100,
						MCP:    &types.MCPBackend{Targets: targets},
					}},
				}},
			}},
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

func TestEngine_ApplyWritesNewTargetsAndRoutes(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	rendered := mcpRenderedConfig(types.MCPTarget{Name: "dep-1_weather", MCP: &types.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-1"}, rendered); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 1 || names[0] != "dep-1_weather" {
		t.Fatalf("target names = %v, want [dep-1_weather]", names)
	}
}

func TestEngine_ApplyUpsertsExistingTargetByName(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	first := mcpRenderedConfig(types.MCPTarget{Name: "dep-1_weather", MCP: &types.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-1"}, first); err != nil {
		t.Fatalf("Apply(first): %v", err)
	}

	second := mcpRenderedConfig(types.MCPTarget{Name: "dep-1_weather", MCP: &types.MCPTargetSpec{Host: "http://weather:9090/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-1"}, second); err != nil {
		t.Fatalf("Apply(second): %v", err)
	}

	cfg, err := loadConfig(dir, engineTestPort)
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	targets := extractMCPRouteTargets(cfg)
	if len(targets) != 1 {
		t.Fatalf("expected 1 target after upsert, got %d: %+v", len(targets), targets)
	}
	if got := targets[0].MCP.Host; got != "http://weather:9090/mcp" {
		t.Fatalf("target host = %q, want updated host", got)
	}
}

func TestEngine_ApplyPreservesEntriesFromOtherDeployments(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	depA := mcpRenderedConfig(types.MCPTarget{Name: "dep-a_weather", MCP: &types.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-a"}, depA); err != nil {
		t.Fatalf("Apply(dep-a): %v", err)
	}
	depB := mcpRenderedConfig(types.MCPTarget{Name: "dep-b_search", MCP: &types.MCPTargetSpec{Host: "http://search:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-b"}, depB); err != nil {
		t.Fatalf("Apply(dep-b): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 2 {
		t.Fatalf("expected both deployments' targets present, got %v", names)
	}
}

func TestEngine_RemoveStripsOnlyMatchingDeploymentID(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	depA := mcpRenderedConfig(types.MCPTarget{Name: "dep-a_weather", MCP: &types.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-a"}, depA); err != nil {
		t.Fatalf("Apply(dep-a): %v", err)
	}
	depB := mcpRenderedConfig(types.MCPTarget{Name: "dep-b_search", MCP: &types.MCPTargetSpec{Host: "http://search:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-b"}, depB); err != nil {
		t.Fatalf("Apply(dep-b): %v", err)
	}

	if err := engine.Remove(context.Background(), gateway.Target{Name: "dep-a"}); err != nil {
		t.Fatalf("Remove(dep-a): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 1 || names[0] != "dep-b_search" {
		t.Fatalf("target names after remove = %v, want [dep-b_search]", names)
	}
}

func TestEngine_RemoveDoesNotStripDeploymentIDPrefixCollision(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	dep1 := mcpRenderedConfig(types.MCPTarget{Name: "weather-dep-1", MCP: &types.MCPTargetSpec{Host: "http://weather:8080/mcp"}})
	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-1"}, dep1); err != nil {
		t.Fatalf("Apply(dep-1): %v", err)
	}
	dep10 := mcpRenderedConfig(types.MCPTarget{Name: "search-dep-10", MCP: &types.MCPTargetSpec{Host: "http://search:8080/mcp"}})
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

func TestEngine_ApplyNilRenderedIsNoop(t *testing.T) {
	dir := t.TempDir()
	engine := NewEngine(dir, engineTestPort)

	if err := engine.Apply(context.Background(), gateway.Target{Name: "dep-1"}, nil); err != nil {
		t.Fatalf("Apply(nil): %v", err)
	}

	names := loadMCPTargetNames(t, dir)
	if len(names) != 0 {
		t.Fatalf("expected no targets written, got %v", names)
	}
}
