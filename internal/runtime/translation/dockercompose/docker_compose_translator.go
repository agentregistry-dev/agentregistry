package dockercompose

import (
	"context"
	"fmt"
	"sort"

	"github.com/compose-spec/compose-go/v2/types"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/version"
)

type DockerComposeConfig = types.Project

type AiRuntimeConfig struct {
	DockerCompose *DockerComposeConfig
	AgentGateway  *AgentGatewayConfig
}

// Translator is the interface for translating MCPServer objects to AgentGateway objects.
type Translator interface {
	TranslateRuntimeConfig(
		ctx context.Context,
		desired *api.DesiredState,
	) (*AiRuntimeConfig, error)
}

type agentGatewayTranslator struct {
	composeWorkingDir string
	agentGatewayPort  uint16
	projectName       string
}

func NewAgentGatewayTranslator(composeWorkingDir string, agentGatewayPort uint16) Translator {
	return &agentGatewayTranslator{
		composeWorkingDir: composeWorkingDir,
		agentGatewayPort:  agentGatewayPort,
		projectName:       "agentregistry_runtime",
	}
}

func NewAgentGatewayTranslatorWithProjectName(composeWorkingDir string, agentGatewayPort uint16, projectName string) Translator {
	return &agentGatewayTranslator{
		composeWorkingDir: composeWorkingDir,
		agentGatewayPort:  agentGatewayPort,
		projectName:       projectName,
	}
}

func (t *agentGatewayTranslator) TranslateRuntimeConfig(
	ctx context.Context,
	desired *api.DesiredState,
) (*AiRuntimeConfig, error) {

	agentGatewayService, err := t.translateAgentGatewayService()
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway service: %w", err)
	}

	dockerComposeServices := map[string]types.ServiceConfig{
		"agent_gateway": *agentGatewayService,
	}

	for _, mcpServer := range desired.MCPServers {
		// only need to create services for local servers
		if mcpServer.ResourceType != api.ResourceTypeLocal || mcpServer.Local.TransportType == api.TransportTypeStdio {
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
		// only need to create services for local agents
		if agent.ResourceType != api.ResourceTypeLocal {
			continue
		}
		// error if Agent name is not unique
		if _, exists := dockerComposeServices[agent.Name]; exists {
			return nil, fmt.Errorf("duplicate Agent name found: %s", agent.Name)
		}

		serviceConfig, err := t.translateAgentToServiceConfig(agent)
		if err != nil {
			return nil, fmt.Errorf("failed to translate MCPServer %s to service config: %w", agent.Name, err)
		}
		dockerComposeServices[agent.Name] = *serviceConfig
	}

	dockerCompose := &DockerComposeConfig{
		Name:       t.projectName,
		WorkingDir: t.composeWorkingDir,
		Services:   dockerComposeServices,
	}

	gwConfig, err := t.translateAgentGatewayConfig(
		desired.MCPServers,
		desired.Agents,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway config: %w", err)
	}

	return &AiRuntimeConfig{
		DockerCompose: dockerCompose,
		AgentGateway:  gwConfig,
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
	var cmd []string
	if server.Local.Deployment.Cmd != "" {
		cmd = []string{server.Local.Deployment.Cmd}
	}
	cmd = append(
		cmd,
		server.Local.Deployment.Args...,
	)

	var envValues []string
	for k, v := range server.Local.Deployment.Env {
		envValues = append(envValues, fmt.Sprintf("%s=%s", k, v))
	}
	sort.SliceStable(envValues, func(i, j int) bool {
		return envValues[i] < envValues[j]
	})

	return &types.ServiceConfig{
		Name:        server.Name,
		Image:       image,
		Command:     cmd,
		Environment: types.NewMappingWithEquals(envValues),
	}, nil
}

func (t *agentGatewayTranslator) translateAgentToServiceConfig(agent *api.Agent) (*types.ServiceConfig, error) {
	image := agent.Local.Deployment.Image
	if image == "" {
		return nil, fmt.Errorf("image must be specified for Agent %s", agent.Name)
	}
	var cmd []string
	if agent.Local.Deployment.Cmd != "" {
		cmd = []string{agent.Local.Deployment.Cmd}
	}
	cmd = append(
		cmd,
		agent.Local.Deployment.Args...,
	)

	var envValues []string
	for k, v := range agent.Local.Deployment.Env {
		envValues = append(envValues, fmt.Sprintf("%s=%s", k, v))
	}
	sort.SliceStable(envValues, func(i, j int) bool {
		return envValues[i] < envValues[j]
	})

	return &types.ServiceConfig{
		Name:    agent.Name,
		Image:   image,
		Command: cmd,
		Ports: []types.ServicePortConfig{{
			Target:    agent.Local.HTTP.Port,
			Published: fmt.Sprintf("%d", agent.Local.HTTP.Port),
		}},
		Environment: types.NewMappingWithEquals(envValues),
	}, nil
}

func (t *agentGatewayTranslator) translateAgentGatewayConfig(
	servers []*api.MCPServer,
	agents []*api.Agent,
) (*AgentGatewayConfig, error) {
	var mcpTargets []MCPTarget

	for _, server := range servers {
		mcpTarget := MCPTarget{
			Name: server.Name,
		}

		switch server.ResourceType {
		case api.ResourceTypeRemote:
			mcpTarget.SSE = &SSETargetSpec{
				Host: server.Remote.Host,
				Port: server.Remote.Port,
				Path: server.Remote.Path,
			}
		case api.ResourceTypeLocal:
			switch server.Local.TransportType {
			case api.TransportTypeStdio:
				mcpTarget.Stdio = &StdioTargetSpec{
					Cmd:  server.Local.Deployment.Cmd,
					Args: server.Local.Deployment.Args,
					Env:  server.Local.Deployment.Env,
				}
			case api.TransportTypeHTTP:
				httpTransportConfig := server.Local.HTTP
				if httpTransportConfig == nil || httpTransportConfig.Port == 0 {
					return nil, fmt.Errorf("HTTP transport requires a target port")
				}
				mcpTarget.SSE = &SSETargetSpec{
					Host: server.Name,
					Port: httpTransportConfig.Port,
					Path: httpTransportConfig.Path,
				}
			default:
				return nil, fmt.Errorf("unsupported transport type: %s", server.Local.TransportType)
			}
		}

		mcpTargets = append(mcpTargets, mcpTarget)
	}

	// sort for idepmpotence
	sort.SliceStable(mcpTargets, func(i, j int) bool {
		return mcpTargets[i].Name < mcpTargets[j].Name
	})

	var routes []LocalRoute

	if len(mcpTargets) > 0 {
		routes = []LocalRoute{{
			RouteName: "mcp_route",
			Matches: []RouteMatch{
				{
					Path: PathMatch{
						PathPrefix: "/mcp",
					},
				},
			},
			Backends: []RouteBackend{{
				Weight: 100,
				MCP: &MCPBackend{
					Targets: mcpTargets,
				},
			}},
		}}
	}

	// append a unique route for each agent
	for _, agent := range agents {
		var (
			hostname = ""
			port     = uint32(0)
			path     = ""
		)
		switch agent.ResourceType {
		case api.ResourceTypeRemote:
			hostname = agent.Remote.Host
			port = agent.Remote.Port
			path = agent.Remote.Path
		case api.ResourceTypeLocal:
			httpTransportConfig := agent.Local.HTTP
			if httpTransportConfig == nil || httpTransportConfig.Port == 0 {
				return nil, fmt.Errorf("HTTP transport requires a target port")
			}

			hostname = agent.Name
			port = httpTransportConfig.Port
			path = httpTransportConfig.Path
		}

		agentRoute := LocalRoute{
			RouteName: fmt.Sprintf("agent_route_%s", agent.Name),
			Matches: []RouteMatch{
				{
					Path: PathMatch{
						PathPrefix: "/agent/" + agent.Name,
					},
				},
			},
			Backends: []RouteBackend{{
				Weight: 100,
				Host:   mkPtr(fmt.Sprintf("%s:%d", hostname, port)),
			}},
			Policies: &FilterOrPolicy{
				URLRewrite: &URLRewrite{
					Path: &PathRedirect{
						Prefix: path,
					},
				},
				A2A: &A2APolicy{},
			},
		}

		routes = append(routes, agentRoute)
	}

	return &AgentGatewayConfig{
		Config: struct{}{},
		Binds: []LocalBind{
			{
				Port: t.agentGatewayPort,
				Listeners: []LocalListener{
					{
						Name:     "default",
						Protocol: "HTTP",
						Routes:   routes,
					},
				},
			},
		},
	}, nil
}

func mkPtr[T any](v T) *T {
	return &v
}
