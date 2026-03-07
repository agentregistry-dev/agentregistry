package local

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"

	platformshared "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/shared"
	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/compose-spec/compose-go/v2/types"
)

type AgentGatewayTranslator struct {
	composeWorkingDir string
	agentGatewayPort  uint16
	projectName       string
}

func NewAgentGatewayTranslator(composeWorkingDir string, agentGatewayPort uint16) *AgentGatewayTranslator {
	return &AgentGatewayTranslator{
		composeWorkingDir: composeWorkingDir,
		agentGatewayPort:  agentGatewayPort,
		projectName:       "agentregistry_runtime",
	}
}

func NewAgentGatewayTranslatorWithProjectName(composeWorkingDir string, agentGatewayPort uint16, projectName string) *AgentGatewayTranslator {
	return &AgentGatewayTranslator{
		composeWorkingDir: composeWorkingDir,
		agentGatewayPort:  agentGatewayPort,
		projectName:       projectName,
	}
}

func canRunInsideAgentGateway(cmd string) bool {
	return cmd == "npx" || cmd == "uvx"
}

const ociServerPort = 3000

func runtimeMCPServiceName(server *api.MCPServer) string {
	return platformshared.GenerateInternalNameForDeployment(server.Name, server.DeploymentID)
}

func runtimeAgentServiceName(agent *api.Agent) string {
	return platformshared.GenerateInternalNameForDeployment(agent.Name, agent.DeploymentID)
}

func (t *AgentGatewayTranslator) TranslatePlatformConfig(
	ctx context.Context,
	desired *api.DesiredState,
) (*api.LocalPlatformConfig, error) {
	agentGatewayService, err := t.translateAgentGatewayService()
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway service: %w", err)
	}

	dockerComposeServices := map[string]types.ServiceConfig{
		"agent_gateway": *agentGatewayService,
	}

	for _, mcpServer := range desired.MCPServers {
		if mcpServer.MCPServerType != api.MCPServerTypeLocal {
			continue
		}
		if mcpServer.Local.TransportType == api.TransportTypeStdio && canRunInsideAgentGateway(mcpServer.Local.Deployment.Cmd) {
			continue
		}
		serviceName := runtimeMCPServiceName(mcpServer)
		if _, exists := dockerComposeServices[serviceName]; exists {
			return nil, fmt.Errorf("duplicate MCPServer name found: %s", mcpServer.Name)
		}

		serviceConfig, err := t.translateMCPServerToServiceConfig(mcpServer)
		if err != nil {
			return nil, fmt.Errorf("failed to translate MCPServer %s to service config: %w", mcpServer.Name, err)
		}
		dockerComposeServices[serviceName] = *serviceConfig
	}

	for _, agent := range desired.Agents {
		serviceName := runtimeAgentServiceName(agent)
		if _, exists := dockerComposeServices[serviceName]; exists {
			return nil, fmt.Errorf("duplicate Agent name found: %s", agent.Name)
		}

		serviceConfig, err := t.translateAgentToServiceConfig(agent)
		if err != nil {
			return nil, fmt.Errorf("failed to translate Agent %s to service config: %w", agent.Name, err)
		}
		dockerComposeServices[serviceName] = *serviceConfig
	}

	dockerCompose := &api.DockerComposeConfig{
		Name:       t.projectName,
		WorkingDir: t.composeWorkingDir,
		Services:   dockerComposeServices,
	}

	gwConfig, err := t.translateAgentGatewayConfig(desired.MCPServers, desired.Agents)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway config: %w", err)
	}

	return &api.LocalPlatformConfig{
		DockerCompose: dockerCompose,
		AgentGateway:  gwConfig,
	}, nil
}

func (t *AgentGatewayTranslator) translateAgentGatewayService() (*types.ServiceConfig, error) {
	port := t.agentGatewayPort
	if port == 0 {
		return nil, fmt.Errorf("agent gateway port must be specified")
	}

	image := fmt.Sprintf("%s/agentregistry-dev/agentregistry/arctl-agentgateway:%s", version.DockerRegistry, version.Version)
	return &types.ServiceConfig{
		Name:    "agent_gateway",
		Image:   image,
		Command: []string{"-f", "/config/agent-gateway.yaml"},
		Ports: []types.ServicePortConfig{{
			Target:    uint32(port),
			Published: fmt.Sprintf("%d", port),
		}},
		Volumes: []types.ServiceVolumeConfig{{
			Type:   types.VolumeTypeBind,
			Source: t.composeWorkingDir,
			Target: "/config",
		}},
	}, nil
}

func (t *AgentGatewayTranslator) translateMCPServerToServiceConfig(server *api.MCPServer) (*types.ServiceConfig, error) {
	image := server.Local.Deployment.Image
	if image == "" {
		return nil, fmt.Errorf("image must be specified for MCPServer %s or the command must be 'uvx' or 'npx'", server.Name)
	}
	var cmd types.ShellCommand
	if server.Local.Deployment.Cmd != "" {
		cmd = append([]string{server.Local.Deployment.Cmd}, server.Local.Deployment.Args...)
	}

	var envValues []string
	for k, v := range server.Local.Deployment.Env {
		envValues = append(envValues, fmt.Sprintf("%s=%s", k, v))
	}
	if server.Local.TransportType == api.TransportTypeStdio && !canRunInsideAgentGateway(server.Local.Deployment.Cmd) {
		envValues = append(envValues, "HOST=0.0.0.0")
		envValues = append(envValues, "MCP_TRANSPORT_MODE=http")
		envValues = append(envValues, fmt.Sprintf("PORT=%d", ociServerPort))
	}

	slices.SortStableFunc(envValues, func(a, b string) int {
		return cmp.Compare(a, b)
	})

	return &types.ServiceConfig{
		Name:        runtimeMCPServiceName(server),
		Image:       image,
		Command:     cmd,
		Environment: types.NewMappingWithEquals(envValues),
	}, nil
}

func (t *AgentGatewayTranslator) translateAgentToServiceConfig(agent *api.Agent) (*types.ServiceConfig, error) {
	image := agent.Deployment.Image
	if image == "" {
		return nil, fmt.Errorf("image must be specified for Agent %s", agent.Name)
	}

	var envValues []string
	for k, v := range agent.Deployment.Env {
		envValues = append(envValues, fmt.Sprintf("%s=%s", k, v))
	}
	slices.SortStableFunc(envValues, func(a, b string) int {
		return cmp.Compare(a, b)
	})

	port := agent.Deployment.Port
	if port == 0 {
		port = 8080
	}

	var agentConfigDir string
	if agent.Version != "" {
		sanitizedVersion := utils.SanitizeVersion(agent.Version)
		agentConfigDir = filepath.Join(t.composeWorkingDir, agent.Name, sanitizedVersion)
	} else {
		agentConfigDir = filepath.Join(t.composeWorkingDir, agent.Name)
	}

	return &types.ServiceConfig{
		Name:        runtimeAgentServiceName(agent),
		Image:       image,
		Command:     []string{agent.Name, "--local", "--port", fmt.Sprintf("%d", port)},
		Environment: types.NewMappingWithEquals(envValues),
		Ports: []types.ServicePortConfig{{
			Target:    uint32(port),
			Published: fmt.Sprintf("%d", port),
		}},
		Volumes: []types.ServiceVolumeConfig{{
			Type:   types.VolumeTypeBind,
			Source: agentConfigDir,
			Target: "/config",
		}},
	}, nil
}

func (t *AgentGatewayTranslator) translateAgentGatewayConfig(servers []*api.MCPServer, agents []*api.Agent) (*api.AgentGatewayConfig, error) {
	var targets []api.MCPTarget

	for _, server := range servers {
		targetName := runtimeMCPServiceName(server)
		mcpTarget := api.MCPTarget{Name: targetName}

		switch server.MCPServerType {
		case api.MCPServerTypeRemote:
			mcpTarget.SSE = &api.SSETargetSpec{
				Host: server.Remote.Host,
				Port: server.Remote.Port,
				Path: server.Remote.Path,
			}
		case api.MCPServerTypeLocal:
			switch server.Local.TransportType {
			case api.TransportTypeStdio:
				if canRunInsideAgentGateway(server.Local.Deployment.Cmd) {
					mcpTarget.Stdio = &api.StdioTargetSpec{
						Cmd:  server.Local.Deployment.Cmd,
						Args: server.Local.Deployment.Args,
						Env:  server.Local.Deployment.Env,
					}
				} else {
					mcpTarget.MCP = &api.MCPTargetSpec{
						Host: fmt.Sprintf("http://%s:%d/mcp", targetName, ociServerPort),
					}
				}
			case api.TransportTypeHTTP:
				httpTransportConfig := server.Local.HTTP
				if httpTransportConfig == nil || httpTransportConfig.Port == 0 {
					return nil, fmt.Errorf("HTTP transport requires a target port")
				}
				mcpTarget.SSE = &api.SSETargetSpec{
					Host: targetName,
					Port: httpTransportConfig.Port,
					Path: httpTransportConfig.Path,
				}
			default:
				return nil, fmt.Errorf("unsupported transport type: %s", server.Local.TransportType)
			}
		}

		targets = append(targets, mcpTarget)
	}

	var agentRoutes []api.LocalRoute
	for _, agent := range agents {
		agentServiceName := runtimeAgentServiceName(agent)
		route := api.LocalRoute{
			RouteName: fmt.Sprintf("%s_route", agentServiceName),
			Matches: []api.RouteMatch{{
				Path: api.PathMatch{
					PathPrefix: fmt.Sprintf("/agents/%s", agentServiceName),
				},
			}},
			Backends: []api.RouteBackend{{
				Weight: 100,
				Host:   fmt.Sprintf("%s:%d", agentServiceName, agent.Deployment.Port),
			}},
			Policies: &api.FilterOrPolicy{
				A2A: &api.A2APolicy{},
				URLRewrite: &api.URLRewrite{
					Path: &api.PathRedirect{Prefix: "/"},
				},
			},
		}
		agentRoutes = append(agentRoutes, route)
	}

	slices.SortStableFunc(agentRoutes, func(a, b api.LocalRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})
	slices.SortStableFunc(targets, func(a, b api.MCPTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})

	mcpRoute := api.LocalRoute{
		RouteName: "mcp_route",
		Matches: []api.RouteMatch{{
			Path: api.PathMatch{
				PathPrefix: "/mcp",
			},
		}},
		Backends: []api.RouteBackend{{
			Weight: 100,
			MCP: &api.MCPBackend{
				Targets: targets,
			},
		}},
	}

	var allRoutes []api.LocalRoute
	if len(targets) > 0 {
		allRoutes = append([]api.LocalRoute{}, mcpRoute)
	}
	allRoutes = append(allRoutes, agentRoutes...)

	return &api.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []api.LocalBind{{
			Port: t.agentGatewayPort,
			Listeners: []api.LocalListener{{
				Name:     "default",
				Protocol: "HTTP",
				Routes:   allRoutes,
			}},
		}},
	}, nil
}
