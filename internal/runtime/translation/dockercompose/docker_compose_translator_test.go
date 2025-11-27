package dockercompose

import (
	"context"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/compose-spec/compose-go/v2/types"
)

func TestNewAgentGatewayTranslator(t *testing.T) {
	translator := NewAgentGatewayTranslator("/tmp/test", 8080)
	if translator == nil {
		t.Fatal("expected non-nil translator")
	}

	// Type assertion to check internal fields
	agTranslator, ok := translator.(*agentGatewayTranslator)
	if !ok {
		t.Fatal("expected *agentGatewayTranslator type")
	}

	if agTranslator.composeWorkingDir != "/tmp/test" {
		t.Errorf("expected composeWorkingDir=/tmp/test, got %s", agTranslator.composeWorkingDir)
	}

	if agTranslator.agentGatewayPort != 8080 {
		t.Errorf("expected agentGatewayPort=8080, got %d", agTranslator.agentGatewayPort)
	}

	if agTranslator.projectName != "agentregistry_runtime" {
		t.Errorf("expected projectName=agentregistry_runtime, got %s", agTranslator.projectName)
	}
}

func TestNewAgentGatewayTranslatorWithProjectName(t *testing.T) {
	translator := NewAgentGatewayTranslatorWithProjectName("/tmp/test", 9090, "custom-project")
	agTranslator := translator.(*agentGatewayTranslator)

	if agTranslator.projectName != "custom-project" {
		t.Errorf("expected projectName=custom-project, got %s", agTranslator.projectName)
	}

	if agTranslator.agentGatewayPort != 9090 {
		t.Errorf("expected agentGatewayPort=9090, got %d", agTranslator.agentGatewayPort)
	}
}

func TestTranslateAgentGatewayService(t *testing.T) {
	tests := []struct {
		name        string
		port        uint16
		workingDir  string
		expectError bool
	}{
		{
			name:        "valid configuration",
			port:        8080,
			workingDir:  "/tmp/runtime",
			expectError: false,
		},
		{
			name:        "zero port should error",
			port:        0,
			workingDir:  "/tmp/runtime",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := &agentGatewayTranslator{
				composeWorkingDir: tt.workingDir,
				agentGatewayPort:  tt.port,
				projectName:       "test-project",
			}

			service, err := translator.translateAgentGatewayService()

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if service.Name != "agent_gateway" {
				t.Errorf("expected service name 'agent_gateway', got %s", service.Name)
			}

			if len(service.Ports) != 1 {
				t.Fatalf("expected 1 port, got %d", len(service.Ports))
			}

			if service.Ports[0].Target != uint32(tt.port) {
				t.Errorf("expected port target %d, got %d", tt.port, service.Ports[0].Target)
			}

			if len(service.Volumes) != 1 {
				t.Fatalf("expected 1 volume, got %d", len(service.Volumes))
			}

			if service.Volumes[0].Source != tt.workingDir {
				t.Errorf("expected volume source %s, got %s", tt.workingDir, service.Volumes[0].Source)
			}

			if service.Volumes[0].Target != "/config" {
				t.Errorf("expected volume target /config, got %s", service.Volumes[0].Target)
			}
		})
	}
}

func TestTranslateMCPServerToServiceConfig(t *testing.T) {
	tests := []struct {
		name        string
		server      *api.MCPServer
		expectError bool
		checkFunc   func(t *testing.T, service *types.ServiceConfig)
	}{
		{
			name: "valid stdio server",
			server: &api.MCPServer{
				Name:          "test-server",
				MCPServerType: api.MCPServerTypeLocal,
				Local: &api.LocalMCPServer{
					Deployment: api.MCPServerDeployment{
						Image: "node:latest",
						Cmd:   "npx",
						Args:  []string{"-y", "@test/server"},
						Env: map[string]string{
							"ENV_VAR1": "value1",
							"ENV_VAR2": "value2",
						},
					},
					TransportType: api.TransportTypeStdio,
				},
			},
			expectError: false,
			checkFunc: func(t *testing.T, service *types.ServiceConfig) {
				if service.Name != "test-server" {
					t.Errorf("expected name test-server, got %s", service.Name)
				}
				if service.Image != "node:latest" {
					t.Errorf("expected image node:latest, got %s", service.Image)
				}
				if len(service.Command) != 3 {
					t.Errorf("expected 3 command parts, got %d", len(service.Command))
				}
				if service.Command[0] != "npx" {
					t.Errorf("expected command[0]=npx, got %s", service.Command[0])
				}
				// Check environment is sorted
				envMap := service.Environment
				if len(envMap) != 2 {
					t.Errorf("expected 2 env vars, got %d", len(envMap))
				}
			},
		},
		{
			name: "missing image",
			server: &api.MCPServer{
				Name:          "test-server",
				MCPServerType: api.MCPServerTypeLocal,
				Local: &api.LocalMCPServer{
					Deployment: api.MCPServerDeployment{
						Cmd:  "npx",
						Args: []string{"-y", "@test/server"},
					},
					TransportType: api.TransportTypeStdio,
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := &agentGatewayTranslator{
				composeWorkingDir: "/tmp/test",
				agentGatewayPort:  8080,
				projectName:       "test-project",
			}

			service, err := translator.translateMCPServerToServiceConfig(tt.server)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, service)
			}
		})
	}
}

func TestTranslateAgentToServiceConfig(t *testing.T) {
	tests := []struct {
		name        string
		agent       *api.Agent
		expectError bool
		checkFunc   func(t *testing.T, service *types.ServiceConfig)
	}{
		{
			name: "agent with default port",
			agent: &api.Agent{
				Name: "default-port-agent",
				Deployment: api.AgentDeployment{
					Image: "test-image:v1",
					Port:  0, // Should default to 8080
				},
			},
			expectError: false,
			checkFunc: func(t *testing.T, service *types.ServiceConfig) {
				if len(service.Ports) != 1 {
					t.Fatalf("expected 1 port, got %d", len(service.Ports))
				}
				if service.Ports[0].Target != 8080 {
					t.Errorf("expected default port 8080, got %d", service.Ports[0].Target)
				}
			},
		},
		{
			name: "agent without image",
			agent: &api.Agent{
				Name: "no-image-agent",
				Deployment: api.AgentDeployment{
					Port: 8080,
				},
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := &agentGatewayTranslator{
				composeWorkingDir: "/tmp/test",
				agentGatewayPort:  8080,
				projectName:       "test-project",
			}

			service, err := translator.translateAgentToServiceConfig(tt.agent)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, service)
			}
		})
	}
}

func TestTranslateAgentGatewayConfig(t *testing.T) {
	tests := []struct {
		name      string
		servers   []*api.MCPServer
		agents    []*api.Agent
		checkFunc func(t *testing.T, config *api.AgentGatewayConfig)
	}{
		{
			name: "remote server only",
			servers: []*api.MCPServer{
				{
					Name:          "remote-server",
					MCPServerType: api.MCPServerTypeRemote,
					Remote: &api.RemoteMCPServer{
						Host: "example.com",
						Port: 443,
						Path: "/api/mcp",
					},
				},
			},
			agents: nil,
			checkFunc: func(t *testing.T, config *api.AgentGatewayConfig) {
				if len(config.Binds) != 1 {
					t.Fatalf("expected 1 bind, got %d", len(config.Binds))
				}
				if len(config.Binds[0].Listeners) != 1 {
					t.Fatalf("expected 1 listener, got %d", len(config.Binds[0].Listeners))
				}
				routes := config.Binds[0].Listeners[0].Routes
				// Should have MCP route only
				if len(routes) != 1 {
					t.Fatalf("expected 1 route (mcp), got %d", len(routes))
				}
				if routes[0].RouteName != "mcp_route" {
					t.Errorf("expected mcp_route, got %s", routes[0].RouteName)
				}
			},
		},
		{
			name: "stdio server only",
			servers: []*api.MCPServer{
				{
					Name:          "stdio-server",
					MCPServerType: api.MCPServerTypeLocal,
					Local: &api.LocalMCPServer{
						Deployment: api.MCPServerDeployment{
							Cmd:  "npx",
							Args: []string{"-y", "@test/server"},
							Env:  map[string]string{"VAR": "value"},
						},
						TransportType: api.TransportTypeStdio,
					},
				},
			},
			agents: nil,
			checkFunc: func(t *testing.T, config *api.AgentGatewayConfig) {
				routes := config.Binds[0].Listeners[0].Routes
				if len(routes) != 1 {
					t.Fatalf("expected 1 route, got %d", len(routes))
				}
				if routes[0].Backends[0].MCP == nil {
					t.Fatal("expected MCP backend")
				}
				if len(routes[0].Backends[0].MCP.Targets) != 1 {
					t.Fatalf("expected 1 target, got %d", len(routes[0].Backends[0].MCP.Targets))
				}
				target := routes[0].Backends[0].MCP.Targets[0]
				if target.Stdio == nil {
					t.Fatal("expected stdio target")
				}
				if target.Stdio.Cmd != "npx" {
					t.Errorf("expected cmd=npx, got %s", target.Stdio.Cmd)
				}
			},
		},
		{
			name: "http transport server",
			servers: []*api.MCPServer{
				{
					Name:          "http-server",
					MCPServerType: api.MCPServerTypeLocal,
					Local: &api.LocalMCPServer{
						Deployment: api.MCPServerDeployment{
							Image: "test:latest",
							Cmd:   "server",
						},
						TransportType: api.TransportTypeHTTP,
						HTTP: &api.HTTPTransport{
							Port: 3000,
							Path: "/mcp",
						},
					},
				},
			},
			agents: nil,
			checkFunc: func(t *testing.T, config *api.AgentGatewayConfig) {
				routes := config.Binds[0].Listeners[0].Routes
				target := routes[0].Backends[0].MCP.Targets[0]
				if target.SSE == nil {
					t.Fatal("expected SSE target for HTTP transport")
				}
				if target.SSE.Host != "http-server" {
					t.Errorf("expected host=http-server, got %s", target.SSE.Host)
				}
				if target.SSE.Port != 3000 {
					t.Errorf("expected port=3000, got %d", target.SSE.Port)
				}
			},
		},
		{
			name:    "agent only",
			servers: nil,
			agents: []*api.Agent{
				{
					Name: "my-agent",
					Deployment: api.AgentDeployment{
						Image: "agent:v1",
						Port:  8080,
					},
				},
			},
			checkFunc: func(t *testing.T, config *api.AgentGatewayConfig) {
				routes := config.Binds[0].Listeners[0].Routes
				// Should have agent route only (no MCP route when no servers)
				if len(routes) != 1 {
					t.Fatalf("expected 1 route (agent), got %d", len(routes))
				}
				if routes[0].RouteName != "my-agent_route" {
					t.Errorf("expected my-agent_route, got %s", routes[0].RouteName)
				}
				if routes[0].Backends[0].Host != "my-agent:8080" {
					t.Errorf("expected host my-agent:8080, got %s", routes[0].Backends[0].Host)
				}
				if routes[0].Policies == nil || routes[0].Policies.A2A == nil {
					t.Error("expected A2A policy")
				}
			},
		},
		{
			name: "multiple agents and servers",
			servers: []*api.MCPServer{
				{
					Name:          "server-a",
					MCPServerType: api.MCPServerTypeRemote,
					Remote: &api.RemoteMCPServer{
						Host: "example.com",
						Port: 443,
						Path: "/api",
					},
				},
			},
			agents: []*api.Agent{
				{
					Name: "agent-b",
					Deployment: api.AgentDeployment{
						Image: "agent:v1",
						Port:  8080,
					},
				},
				{
					Name: "agent-a",
					Deployment: api.AgentDeployment{
						Image: "agent:v2",
						Port:  8081,
					},
				},
			},
			checkFunc: func(t *testing.T, config *api.AgentGatewayConfig) {
				routes := config.Binds[0].Listeners[0].Routes
				// 1 MCP route + 2 agent routes = 3 total
				if len(routes) != 3 {
					t.Fatalf("expected 3 routes, got %d", len(routes))
				}
				// First should be MCP
				if routes[0].RouteName != "mcp_route" {
					t.Errorf("expected first route to be mcp_route, got %s", routes[0].RouteName)
				}
				// Agents should be sorted alphabetically
				if routes[1].RouteName != "agent-a_route" {
					t.Errorf("expected second route to be agent-a_route, got %s", routes[1].RouteName)
				}
				if routes[2].RouteName != "agent-b_route" {
					t.Errorf("expected third route to be agent-b_route, got %s", routes[2].RouteName)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := &agentGatewayTranslator{
				composeWorkingDir: "/tmp/test",
				agentGatewayPort:  8080,
				projectName:       "test-project",
			}

			config, err := translator.translateAgentGatewayConfig(tt.servers, tt.agents)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, config)
			}
		})
	}
}

func TestTranslateRuntimeConfig(t *testing.T) {
	tests := []struct {
		name         string
		desiredState *api.DesiredState
		expectError  bool
		errorMsg     string
		checkFunc    func(t *testing.T, config *api.AIRuntimeConfig)
	}{
		{
			name: "empty desired state",
			desiredState: &api.DesiredState{
				MCPServers: nil,
				Agents:     nil,
			},
			expectError: false,
			checkFunc: func(t *testing.T, config *api.AIRuntimeConfig) {
				if config.Type != api.RuntimeConfigTypeLocal {
					t.Errorf("expected local runtime type, got %s", config.Type)
				}
				if config.Local == nil {
					t.Fatal("expected local config to be non-nil")
				}
				if config.Local.DockerCompose == nil {
					t.Fatal("expected docker compose config")
				}
				// Should have agent_gateway service
				if len(config.Local.DockerCompose.Services) != 1 {
					t.Errorf("expected 1 service (agent_gateway), got %d", len(config.Local.DockerCompose.Services))
				}
			},
		},
		{
			name: "duplicate server names",
			desiredState: &api.DesiredState{
				MCPServers: []*api.MCPServer{
					{
						Name:          "duplicate",
						MCPServerType: api.MCPServerTypeLocal,
						Local: &api.LocalMCPServer{
							Deployment: api.MCPServerDeployment{
								Image: "test:v1",
								Cmd:   "cmd",
							},
							TransportType: api.TransportTypeHTTP,
							HTTP: &api.HTTPTransport{
								Port: 3000,
							},
						},
					},
					{
						Name:          "duplicate",
						MCPServerType: api.MCPServerTypeLocal,
						Local: &api.LocalMCPServer{
							Deployment: api.MCPServerDeployment{
								Image: "test:v2",
								Cmd:   "cmd",
							},
							TransportType: api.TransportTypeHTTP,
							HTTP: &api.HTTPTransport{
								Port: 3001,
							},
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "duplicate MCPServer name",
		},
		{
			name: "duplicate agent names",
			desiredState: &api.DesiredState{
				Agents: []*api.Agent{
					{
						Name: "duplicate",
						Deployment: api.AgentDeployment{
							Image: "agent:v1",
							Port:  8080,
						},
					},
					{
						Name: "duplicate",
						Deployment: api.AgentDeployment{
							Image: "agent:v2",
							Port:  8081,
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "duplicate Agent name",
		},
		{
			name: "agent and server with same name",
			desiredState: &api.DesiredState{
				MCPServers: []*api.MCPServer{
					{
						Name:          "same-name",
						MCPServerType: api.MCPServerTypeLocal,
						Local: &api.LocalMCPServer{
							Deployment: api.MCPServerDeployment{
								Image: "server:v1",
								Cmd:   "cmd",
							},
							TransportType: api.TransportTypeHTTP,
							HTTP: &api.HTTPTransport{
								Port: 3000,
							},
						},
					},
				},
				Agents: []*api.Agent{
					{
						Name: "same-name",
						Deployment: api.AgentDeployment{
							Image: "agent:v1",
							Port:  8080,
						},
					},
				},
			},
			expectError: true,
			errorMsg:    "duplicate Agent name",
		},
		{
			name: "complete configuration",
			desiredState: &api.DesiredState{
				MCPServers: []*api.MCPServer{
					{
						Name:          "mcp-server-1",
						MCPServerType: api.MCPServerTypeLocal,
						Local: &api.LocalMCPServer{
							Deployment: api.MCPServerDeployment{
								Image: "mcp:v1",
								Cmd:   "server",
								Args:  []string{"--port", "3000"},
								Env: map[string]string{
									"KEY": "value",
								},
							},
							TransportType: api.TransportTypeHTTP,
							HTTP: &api.HTTPTransport{
								Port: 3000,
								Path: "/api",
							},
						},
					},
				},
				Agents: []*api.Agent{
					{
						Name: "agent-1",
						Deployment: api.AgentDeployment{
							Image: "agent:v1",
							Port:  8080,
							Env: map[string]string{
								"MODEL_PROVIDER": "anthropic",
							},
						},
					},
				},
			},
			expectError: false,
			checkFunc: func(t *testing.T, config *api.AIRuntimeConfig) {
				// Should have 3 services: agent_gateway, mcp-server-1, agent-1
				if len(config.Local.DockerCompose.Services) != 3 {
					t.Errorf("expected 3 services, got %d", len(config.Local.DockerCompose.Services))
				}

				// Check service names
				if _, ok := config.Local.DockerCompose.Services["agent_gateway"]; !ok {
					t.Error("missing agent_gateway service")
				}
				if _, ok := config.Local.DockerCompose.Services["mcp-server-1"]; !ok {
					t.Error("missing mcp-server-1 service")
				}
				if _, ok := config.Local.DockerCompose.Services["agent-1"]; !ok {
					t.Error("missing agent-1 service")
				}

				// Check gateway config
				if config.Local.AgentGateway == nil {
					t.Fatal("expected agent gateway config")
				}
				routes := config.Local.AgentGateway.Binds[0].Listeners[0].Routes
				// Should have 2 routes: mcp_route and agent-1_route
				if len(routes) != 2 {
					t.Errorf("expected 2 routes, got %d", len(routes))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			translator := &agentGatewayTranslator{
				composeWorkingDir: "/tmp/test",
				agentGatewayPort:  8080,
				projectName:       "test-project",
			}

			config, err := translator.TranslateRuntimeConfig(context.Background(), tt.desiredState)

			if tt.expectError {
				if err == nil {
					t.Fatal("expected error but got nil")
				}
				if tt.errorMsg != "" && err.Error() != "" {
					// Just check that error message contains expected substring
					if !contains(err.Error(), tt.errorMsg) {
						t.Errorf("expected error containing %q, got %q", tt.errorMsg, err.Error())
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.checkFunc != nil {
				tt.checkFunc(t, config)
			}
		})
	}
}

func TestTranslateAgentGatewayConfig_HTTPTransportWithoutPort(t *testing.T) {
	translator := &agentGatewayTranslator{
		composeWorkingDir: "/tmp/test",
		agentGatewayPort:  8080,
		projectName:       "test-project",
	}

	servers := []*api.MCPServer{
		{
			Name:          "http-server-no-port",
			MCPServerType: api.MCPServerTypeLocal,
			Local: &api.LocalMCPServer{
				Deployment: api.MCPServerDeployment{
					Image: "test:latest",
					Cmd:   "server",
				},
				TransportType: api.TransportTypeHTTP,
				HTTP:          nil, // Missing HTTP config
			},
		},
	}

	_, err := translator.translateAgentGatewayConfig(servers, nil)
	if err == nil {
		t.Fatal("expected error for HTTP transport without port")
	}
	if !contains(err.Error(), "HTTP transport requires a target port") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestTranslateAgentGatewayConfig_UnsupportedTransportType(t *testing.T) {
	translator := &agentGatewayTranslator{
		composeWorkingDir: "/tmp/test",
		agentGatewayPort:  8080,
		projectName:       "test-project",
	}

	servers := []*api.MCPServer{
		{
			Name:          "invalid-transport",
			MCPServerType: api.MCPServerTypeLocal,
			Local: &api.LocalMCPServer{
				Deployment: api.MCPServerDeployment{
					Image: "test:latest",
					Cmd:   "server",
				},
				TransportType: "unknown", // Invalid transport type
			},
		},
	}

	_, err := translator.translateAgentGatewayConfig(servers, nil)
	if err == nil {
		t.Fatal("expected error for unsupported transport type")
	}
	if !contains(err.Error(), "unsupported transport type") {
		t.Errorf("unexpected error message: %v", err)
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
