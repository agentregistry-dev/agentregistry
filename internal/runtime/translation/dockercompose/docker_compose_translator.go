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

	dockerCompose := &DockerComposeConfig{
		Name:       t.projectName,
		WorkingDir: t.composeWorkingDir,
		Services:   dockerComposeServices,
	}

	gwConfig, err := t.translateAgentGatewayConfig(desired.MCPServers)
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
	image := fmt.Sprintf("localhost:5001/agentregistry-dev/agentregistry/arctl-agentgateway:%s", version.Version)

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

func (t *agentGatewayTranslator) translateAgentGatewayConfig(servers []*api.MCPServer) (*AgentGatewayConfig, error) {
	var targets []MCPTarget

	for _, server := range servers {
		mcpTarget := MCPTarget{
			Name: server.Name,
		}

		switch server.MCPServerType {
		case api.MCPServerTypeRemote:
			mcpTarget.SSE = &SSETargetSpec{
				Host: server.Remote.Host,
				Port: server.Remote.Port,
				Path: server.Remote.Path,
			}
		case api.MCPServerTypeLocal:
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

		targets = append(targets, mcpTarget)
	}

	// sort for idepmpotence
	sort.SliceStable(targets, func(i, j int) bool {
		return targets[i].Name < targets[j].Name
	})

	return &AgentGatewayConfig{
		Config: struct{}{},
		Binds: []LocalBind{
			{
				Port: t.agentGatewayPort,
				Listeners: []LocalListener{
					{
						Name:     "default",
						Protocol: "HTTP",
						Routes: []LocalRoute{{
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
									Targets: targets,
								},
							}},
						}},
					},
				},
			},
		},
	}, nil
}
