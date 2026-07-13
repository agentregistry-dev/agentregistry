package gateway

import (
	"context"
	"reflect"
	"testing"

	types "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

// nativeConfig unwraps a RenderedConfig produced by AgentGatewayEngine so tests
// can compare the whole native object. It fails the test if rendered was not
// produced by this engine.
func nativeConfig(t *testing.T, rendered RenderedConfig) *types.AgentGatewayConfig {
	t.Helper()
	rc, ok := rendered.(renderedAgentGatewayConfig)
	if !ok {
		t.Fatalf("rendered config has unexpected type %T", rendered)
	}
	return rc.config
}

func TestAgentGatewayEngine_Render(t *testing.T) {
	tests := []struct {
		name    string
		desired Config
		want    *types.AgentGatewayConfig
	}{
		{
			name:    "empty config renders no binds",
			desired: Config{},
			want:    &types.AgentGatewayConfig{Config: struct{}{}},
		},
		{
			name: "single http listener",
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
					Name:     "https",
					Protocol: "HTTPS",
					Port:     8443,
					TLS: &TLSConfig{
						Mode: "Terminate",
						CertificateRefs: []ObjectRef{{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
					Name:     "https",
					Protocol: "HTTPS",
					Port:     8443,
					TLS: &TLSConfig{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					AllowedRoutes: &AllowedRoutes{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
				}},
				Routes: []Route{{
					Name:       "api",
					Hostnames:  []string{"example.com"},
					PathPrefix: "/api",
					BackendRefs: []BackendRef{{
						Name: "svc",
					}},
				}},
				Backends: []Backend{{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					Policies: []PolicyRef{{Name: "mcp-authz"}},
				}},
				Policies: []Policy{{
					Name: "mcp-authz",
					Type: "MCPAuthorization",
					Spec: PolicySpec{
						MCPAuthorization: &AuthzPolicy{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					Policies: []PolicyRef{{Name: "traffic-authz"}},
				}},
				Policies: []Policy{{
					Name: "traffic-authz",
					Type: "TrafficAuthorization",
					Spec: PolicySpec{
						TrafficAuthorization: &AuthzPolicy{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{{
					Name:     "http",
					Protocol: "HTTP",
					Port:     8080,
					Policies: []PolicyRef{{Name: "connect"}},
				}},
				Policies: []Policy{{
					Name: "connect",
					Type: "FrontendConnect",
					Spec: PolicySpec{
						FrontendConnect: &FrontendConnectPolicy{
							Enabled: true,
							Authorization: &AuthzPolicy{
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
			desired: Config{
				ClassName: "agentgateway",
				Listeners: []Listener{
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
	}

	engine := NewAgentGatewayEngine(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered, err := engine.Render(context.Background(), tt.desired)
			if err != nil {
				t.Fatalf("Render() unexpected error: %v", err)
			}
			got := nativeConfig(t, rendered)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Render() mismatch\n got: %#v\nwant: %#v", got, tt.want)
			}
		})
	}
}

func TestAgentGatewayEngine_RenderIsDeterministic(t *testing.T) {
	desired := Config{
		ClassName: "agentgateway",
		Listeners: []Listener{
			{Name: "b", Protocol: "HTTP", Port: 9000},
			{Name: "a", Protocol: "HTTP", Port: 8000},
		},
		Routes: []Route{
			{Name: "z", PathPrefix: "/z"},
			{Name: "a", PathPrefix: "/a"},
		},
	}
	engine := NewAgentGatewayEngine(nil)

	first, err := engine.Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	second, err := engine.Render(context.Background(), desired)
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}

	if !reflect.DeepEqual(nativeConfig(t, first), nativeConfig(t, second)) {
		t.Errorf("Render() is not deterministic across identical inputs")
	}
}

// recordingApplier captures the arguments of the most recent Apply/Remove call.
type recordingApplier struct {
	applyTarget  Target
	applyConfig  *types.AgentGatewayConfig
	removeTarget Target
	removeCalled bool
}

func (r *recordingApplier) Apply(_ context.Context, target Target, cfg *types.AgentGatewayConfig) error {
	r.applyTarget = target
	r.applyConfig = cfg
	return nil
}

func (r *recordingApplier) Remove(_ context.Context, target Target) error {
	r.removeTarget = target
	r.removeCalled = true
	return nil
}

// foreignRendered is a RenderedConfig not produced by AgentGatewayEngine.
type foreignRendered struct{}

func (foreignRendered) isRenderedConfig() {}

func TestAgentGatewayEngine_ApplyDelegatesToApplier(t *testing.T) {
	applier := &recordingApplier{}
	engine := NewAgentGatewayEngine(applier)

	rendered, err := engine.Render(context.Background(), Config{
		ClassName: "agentgateway",
		Listeners: []Listener{{Name: "http", Protocol: "HTTP", Port: 8080}},
	})
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}

	target := Target{Name: "gw", UID: "uid-1"}
	if err := engine.Apply(context.Background(), target, rendered); err != nil {
		t.Fatalf("Apply() unexpected error: %v", err)
	}

	if applier.applyTarget != target {
		t.Errorf("Apply() target = %+v, want %+v", applier.applyTarget, target)
	}
	if !reflect.DeepEqual(applier.applyConfig, nativeConfig(t, rendered)) {
		t.Errorf("Apply() passed config %#v, want %#v", applier.applyConfig, nativeConfig(t, rendered))
	}
}

func TestAgentGatewayEngine_RemoveDelegatesToApplier(t *testing.T) {
	applier := &recordingApplier{}
	engine := NewAgentGatewayEngine(applier)

	target := Target{Name: "gw", UID: "uid-1"}
	if err := engine.Remove(context.Background(), target); err != nil {
		t.Fatalf("Remove() unexpected error: %v", err)
	}

	if !applier.removeCalled {
		t.Fatalf("Remove() did not call applier")
	}
	if applier.removeTarget != target {
		t.Errorf("Remove() target = %+v, want %+v", applier.removeTarget, target)
	}
}

func TestAgentGatewayEngine_ApplyRejectsForeignRenderedConfig(t *testing.T) {
	engine := NewAgentGatewayEngine(&recordingApplier{})
	if err := engine.Apply(context.Background(), Target{Name: "gw"}, foreignRendered{}); err == nil {
		t.Fatal("Apply() with foreign RenderedConfig should return an error")
	}
}

func TestAgentGatewayEngine_ApplyErrorsWithoutApplier(t *testing.T) {
	engine := NewAgentGatewayEngine(nil)
	rendered, err := engine.Render(context.Background(), Config{})
	if err != nil {
		t.Fatalf("Render() unexpected error: %v", err)
	}
	if err := engine.Apply(context.Background(), Target{Name: "gw"}, rendered); err == nil {
		t.Fatal("Apply() with nil applier should return an error")
	}
}

func TestAgentGatewayEngine_RemoveErrorsWithoutApplier(t *testing.T) {
	engine := NewAgentGatewayEngine(nil)
	if err := engine.Remove(context.Background(), Target{Name: "gw"}); err == nil {
		t.Fatal("Remove() with nil applier should return an error")
	}
}
