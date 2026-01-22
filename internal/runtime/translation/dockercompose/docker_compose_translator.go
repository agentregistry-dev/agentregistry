package dockercompose

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"

	api "github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/compose-spec/compose-go/v2/types"
)

type agentGatewayTranslator struct {
	composeWorkingDir string
	agentGatewayPort  uint16
	projectName       string
}

func NewAgentGatewayTranslator(composeWorkingDir string, agentGatewayPort uint16) api.RuntimeTranslator {
	return &agentGatewayTranslator{
		composeWorkingDir: composeWorkingDir,
		agentGatewayPort:  agentGatewayPort,
		projectName:       "agentregistry_runtime",
	}
}

func NewAgentGatewayTranslatorWithProjectName(composeWorkingDir string, agentGatewayPort uint16, projectName string) api.RuntimeTranslator {
	return &agentGatewayTranslator{
		composeWorkingDir: composeWorkingDir,
		agentGatewayPort:  agentGatewayPort,
		projectName:       projectName,
	}
}

func (t *agentGatewayTranslator) TranslateRuntimeConfig(
	ctx context.Context,
	desired *api.DesiredState,
) (*api.AIRuntimeConfig, error) {
	agentGatewayService, err := t.translateAgentGatewayService()
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway service: %w", err)
	}

	dockerComposeServices := map[string]types.ServiceConfig{
		"agent_gateway": *agentGatewayService,
	}

	for _, mcpServer := range desired.MCPServers {
		// only need to create services for local servers
		if mcpServer.MCPServerType != api.MCPServerTypeLocal || mcpServer.Local.TransportType == api.TransportTypeStdio {
			continue
		}
		// error if MCPServer name is not unique
		if _, exists := dockerComposeServices[mcpServer.Name]; exists {
			return nil, fmt.Errorf("duplicate MCPServer name found: %s", mcpServer.Name)
		}

		serviceConfig, err := t.translateMCPServerToServiceConfig(mcpServer)
		if err != nil {
			return nil, fmt.Errorf("failed to translate MCPServer %s to service config: %w", mcpServer.Name, err)
		}
		dockerComposeServices[mcpServer.Name] = *serviceConfig
	}

	for _, agent := range desired.Agents {
		if _, exists := dockerComposeServices[agent.Name]; exists {
			return nil, fmt.Errorf("duplicate Agent name found: %s", agent.Name)
		}

		serviceConfig, err := t.translateAgentToServiceConfig(agent)
		if err != nil {
			return nil, fmt.Errorf("failed to translate Agent %s to service config: %w", agent.Name, err)
		}
		dockerComposeServices[agent.Name] = *serviceConfig
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

	return &api.AIRuntimeConfig{
		Type: api.RuntimeConfigTypeLocal,
		Local: &api.LocalRuntimeConfig{
			DockerCompose: dockerCompose,
			AgentGateway:  gwConfig,
		},
	}, nil
}

func (t *agentGatewayTranslator) translateAgentGatewayService() (*types.ServiceConfig, error) {
	port := t.agentGatewayPort
	if port == 0 {
		return nil, fmt.Errorf("agent gateway port must be specified")
	}

	// Use custom image with npx and uvx support for stdio MCP servers
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

func (t *agentGatewayTranslator) translateMCPServerToServiceConfig(server *api.MCPServer) (*types.ServiceConfig, error) {
	image := server.Local.Deployment.Image
	if image == "" {
		return nil, fmt.Errorf("image must be specified for MCPServer %s or the command must be 'uvx' or 'npx'", server.Name)
	}
	cmd := append(
		[]string{server.Local.Deployment.Cmd},
		server.Local.Deployment.Args...,
	)

	var envValues []string
	for k, v := range server.Local.Deployment.Env {
		envValues = append(envValues, fmt.Sprintf("%s=%s", k, v))
	}
	slices.SortStableFunc(envValues, func(a, b string) int {
		return cmp.Compare(a, b)
	})

	return &types.ServiceConfig{
		Name:        server.Name,
		Image:       image,
		Command:     cmd,
		Environment: types.NewMappingWithEquals(envValues),
	}, nil
}

func (t *agentGatewayTranslator) translateAgentToServiceConfig(agent *api.Agent) (*types.ServiceConfig, error) {
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
		port = 8080 // default port
	}

	// Mount agent-specific subdirectory: {composeWorkingDir}/{agentName}/{version} -> /config
	// Runtime agents should always have a version, but handle empty gracefully
	var agentConfigDir string
	if agent.Version != "" {
		sanitizedVersion := utils.SanitizeVersion(agent.Version)
		agentConfigDir = filepath.Join(t.composeWorkingDir, agent.Name, sanitizedVersion)
	} else {
		// Fallback to non-versioned directory for safety (shouldn't happen for runtime agents)
		agentConfigDir = filepath.Join(t.composeWorkingDir, agent.Name)
	}

	return &types.ServiceConfig{
		Name:        agent.Name,
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

func (t *agentGatewayTranslator) translateAgentGatewayConfig(servers []*api.MCPServer, agents []*api.Agent) (*api.AgentGatewayConfig, error) {
	var targets []api.MCPTarget

	for _, server := range servers {
		mcpTarget := api.MCPTarget{
			Name: server.Name,
		}

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
				mcpTarget.Stdio = &api.StdioTargetSpec{
					Cmd:  server.Local.Deployment.Cmd,
					Args: server.Local.Deployment.Args,
					Env:  server.Local.Deployment.Env,
				}
			case api.TransportTypeHTTP:
				httpTransportConfig := server.Local.HTTP
				if httpTransportConfig == nil || httpTransportConfig.Port == 0 {
					return nil, fmt.Errorf("HTTP transport requires a target port")
				}
				mcpTarget.SSE = &api.SSETargetSpec{
					Host: server.Name,
					Port: httpTransportConfig.Port,
					Path: httpTransportConfig.Path,
				}
			default:
				return nil, fmt.Errorf("unsupported transport type: %s", server.Local.TransportType)
			}
		}

		targets = append(targets, mcpTarget)
	}

	// create route for each agent
	var agentRoutes []api.LocalRoute
	for _, agent := range agents {
		route := api.LocalRoute{
			RouteName: fmt.Sprintf("%s_route", agent.Name),
			Matches: []api.RouteMatch{
				{
					Path: api.PathMatch{
						PathPrefix: fmt.Sprintf("/agents/%s", agent.Name),
					},
				},
			},
			Backends: []api.RouteBackend{{
				Weight: 100,
				Host:   fmt.Sprintf("%s:%d", agent.Name, agent.Deployment.Port),
			}},
			Policies: &api.FilterOrPolicy{
				A2A: &api.A2APolicy{},
				URLRewrite: &api.URLRewrite{
					Path: &api.PathRedirect{
						Prefix: "/",
					},
				},
			},
		}
		agentRoutes = append(agentRoutes, route)
	}

	// sort for idempotence
	slices.SortStableFunc(agentRoutes, func(a, b api.LocalRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})

	slices.SortStableFunc(targets, func(a, b api.MCPTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})

	mcpRoute := api.LocalRoute{
		RouteName: "mcp_route",
		Matches: []api.RouteMatch{
			{
				Path: api.PathMatch{
					PathPrefix: "/mcp",
				},
			},
		},
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
		Binds: []api.LocalBind{
			{
				Port: t.agentGatewayPort,
				Listeners: []api.LocalListener{
					{
						Name:     "default",
						Protocol: "HTTP",
						Routes:   allRoutes,
					},
				},
			},
		},
	}, nil
}
