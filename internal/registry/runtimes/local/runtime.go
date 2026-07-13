package local

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"go.yaml.in/yaml/v3"

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
	runtimeutils "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
)

// agentRoutePolicyName names the shared policy attaching A2A + URL-rewrite
// semantics to every agent route.
const agentRoutePolicyName = "agent-a2a-rewrite"

const (
	localComposeFileName    = "docker-compose.yaml"
	defaultLocalProjectName = "agentregistry_runtime"
	localOCIServerPort      = 3000
)

func BuildLocalRuntimeConfig(
	ctx context.Context,
	engine gateway.Engine,
	runtimeDir string,
	agentGatewayPort uint16,
	projectName string,
	desired *runtimetypes.DesiredState,
) (*runtimetypes.LocalRuntimeConfig, error) {
	if strings.TrimSpace(projectName) == "" {
		projectName = defaultLocalProjectName
	}

	agentGatewayService, err := translateLocalAgentGatewayService(runtimeDir, agentGatewayPort)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway service: %w", err)
	}

	dockerComposeServices := map[string]composetypes.ServiceConfig{
		"agent_gateway": *agentGatewayService,
	}

	for _, mcpServer := range desired.MCPServers {
		if mcpServer.MCPServerType != runtimetypes.MCPServerTypeLocal {
			continue
		}
		if mcpServer.Local.TransportType == runtimetypes.TransportTypeStdio && canRunInsideLocalAgentGateway(mcpServer.Local.Deployment.Cmd) {
			continue
		}
		serviceName := localMCPServiceName(mcpServer)
		if _, exists := dockerComposeServices[serviceName]; exists {
			return nil, fmt.Errorf("duplicate MCPServer name found: %s", mcpServer.Name)
		}

		serviceConfig, err := translateLocalMCPServerToServiceConfig(mcpServer)
		if err != nil {
			return nil, fmt.Errorf("failed to translate MCPServer %s to service config: %w", mcpServer.Name, err)
		}
		dockerComposeServices[serviceName] = *serviceConfig
	}

	for _, agent := range desired.Agents {
		serviceName := localAgentServiceName(agent)
		if _, exists := dockerComposeServices[serviceName]; exists {
			return nil, fmt.Errorf("duplicate Agent name found: %s", agent.Name)
		}

		serviceConfig, err := translateLocalAgentToServiceConfig(runtimeDir, agent)
		if err != nil {
			return nil, fmt.Errorf("failed to translate Agent %s to service config: %w", agent.Name, err)
		}
		dockerComposeServices[serviceName] = *serviceConfig
	}

	dockerCompose := &runtimetypes.DockerComposeConfig{
		Name:       projectName,
		WorkingDir: runtimeDir,
		Services:   dockerComposeServices,
	}

	gatewayConfig, err := translateLocalAgentGatewayConfig(ctx, engine, agentGatewayPort, desired.MCPServers, desired.Agents)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway config: %w", err)
	}

	return &runtimetypes.LocalRuntimeConfig{
		DockerCompose: dockerCompose,
		AgentGateway:  gatewayConfig,
	}, nil
}

func ComposeUpLocalRuntime(ctx context.Context, runtimeDir string, verbose bool) error {
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "--remove-orphans", "--force-recreate")
	cmd.Dir = runtimeDir
	var stderrBuf bytes.Buffer
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker compose: %w: %s", err, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}

func ComposeDownLocalRuntime(ctx context.Context, runtimeDir string, verbose bool) error {
	if _, err := os.Stat(runtimeDir); os.IsNotExist(err) {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "down", "--remove-orphans")
	cmd.Dir = runtimeDir
	var stderrBuf bytes.Buffer
	if verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	} else {
		cmd.Stderr = &stderrBuf
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop docker compose: %w: %s", err, strings.TrimSpace(stderrBuf.String()))
	}
	return nil
}

func LoadLocalDockerComposeConfig(runtimeDir string) (*runtimetypes.DockerComposeConfig, error) {
	path := filepath.Join(runtimeDir, localComposeFileName)
	project := &runtimetypes.DockerComposeConfig{
		Name:       defaultLocalProjectName,
		WorkingDir: runtimeDir,
		Services:   map[string]composetypes.ServiceConfig{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return project, nil
		}
		return nil, fmt.Errorf("read docker compose config: %w", err)
	}
	if err := yaml.Unmarshal(data, project); err != nil {
		return nil, fmt.Errorf("unmarshal docker compose config: %w", err)
	}
	if project.Name == "" {
		project.Name = defaultLocalProjectName
	}
	if project.WorkingDir == "" {
		project.WorkingDir = runtimeDir
	}
	if project.Services == nil {
		project.Services = map[string]composetypes.ServiceConfig{}
	}
	return project, nil
}

func writeLocalDockerComposeConfig(runtimeDir string, project *runtimetypes.DockerComposeConfig) error {
	if err := os.MkdirAll(runtimeDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if project == nil {
		project = &runtimetypes.DockerComposeConfig{
			Name:       defaultLocalProjectName,
			WorkingDir: runtimeDir,
			Services:   map[string]composetypes.ServiceConfig{},
		}
	}
	if project.Name == "" {
		project.Name = defaultLocalProjectName
	}
	if project.WorkingDir == "" {
		project.WorkingDir = runtimeDir
	}
	if project.Services == nil {
		project.Services = map[string]composetypes.ServiceConfig{}
	}
	content, err := project.MarshalYAML()
	if err != nil {
		return fmt.Errorf("marshal docker compose config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(runtimeDir, localComposeFileName), content, 0644); err != nil {
		return fmt.Errorf("write docker compose config: %w", err)
	}
	return nil
}

func canRunInsideLocalAgentGateway(cmd string) bool {
	return cmd == "npx" || cmd == "uvx"
}

func localMCPServiceName(server *runtimetypes.MCPServer) string {
	return runtimeutils.GenerateInternalNameForDeployment(server.Name, server.DeploymentID)
}

func localAgentServiceName(agent *runtimetypes.Agent) string {
	return runtimeutils.GenerateInternalNameForDeployment(agent.Name, agent.DeploymentID)
}

func translateLocalAgentGatewayService(runtimeDir string, port uint16) (*composetypes.ServiceConfig, error) {
	if port == 0 {
		return nil, fmt.Errorf("agent gateway port must be specified")
	}

	image := fmt.Sprintf("%s/agentregistry-dev/agentregistry/arctl-agentgateway:%s", version.DockerRegistry, version.Version)
	return &composetypes.ServiceConfig{
		Name:    "agent_gateway",
		Image:   image,
		Command: []string{"-f", "/config/agent-gateway.yaml"},
		Ports: []composetypes.ServicePortConfig{{
			Target:    uint32(port),
			Published: fmt.Sprintf("%d", port),
		}},
		Volumes: []composetypes.ServiceVolumeConfig{{
			Type:   composetypes.VolumeTypeBind,
			Source: runtimeDir,
			Target: "/config",
		}},
	}, nil
}

func translateLocalMCPServerToServiceConfig(server *runtimetypes.MCPServer) (*composetypes.ServiceConfig, error) {
	image := server.Local.Deployment.Image
	if image == "" {
		return nil, fmt.Errorf("image must be specified for MCPServer %s or the command must be 'uvx' or 'npx'", server.Name)
	}
	var cmd composetypes.ShellCommand
	if server.Local.Deployment.Cmd != "" {
		cmd = append([]string{server.Local.Deployment.Cmd}, server.Local.Deployment.Args...)
	}

	var envValues []string
	for k, v := range server.Local.Deployment.Env {
		envValues = append(envValues, fmt.Sprintf("%s=%s", k, v))
	}
	if server.Local.TransportType == runtimetypes.TransportTypeStdio && !canRunInsideLocalAgentGateway(server.Local.Deployment.Cmd) {
		envValues = append(envValues, "HOST=0.0.0.0")
		envValues = append(envValues, fmt.Sprintf("PORT=%d", localOCIServerPort))
	}
	slices.SortStableFunc(envValues, func(a, b string) int { return cmp.Compare(a, b) })

	return &composetypes.ServiceConfig{
		Name:        localMCPServiceName(server),
		Image:       image,
		Command:     cmd,
		Environment: composetypes.NewMappingWithEquals(envValues),
	}, nil
}

func translateLocalAgentToServiceConfig(runtimeDir string, agent *runtimetypes.Agent) (*composetypes.ServiceConfig, error) {
	image := agent.Deployment.Image
	if image == "" {
		return nil, fmt.Errorf("image must be specified for Agent %s", agent.Name)
	}

	var envValues []string
	for k, v := range agent.Deployment.Env {
		envValues = append(envValues, fmt.Sprintf("%s=%s", k, v))
	}
	slices.SortStableFunc(envValues, func(a, b string) int { return cmp.Compare(a, b) })

	port := agent.Deployment.Port
	if port == 0 {
		port = runtimeutils.DefaultLocalAgentPort
	}

	var agentConfigDir string
	if agent.Tag != "" {
		sanitizedTag := sanitizeVersion(agent.Tag)
		agentConfigDir = filepath.Join(runtimeDir, agent.Name, sanitizedTag)
	} else {
		agentConfigDir = filepath.Join(runtimeDir, agent.Name)
	}

	return &composetypes.ServiceConfig{
		Name:        localAgentServiceName(agent),
		Image:       image,
		Command:     []string{agent.Name, "--local", "--port", fmt.Sprintf("%d", port)},
		Environment: composetypes.NewMappingWithEquals(envValues),
		Ports: []composetypes.ServicePortConfig{{
			Target:    uint32(port),
			Published: fmt.Sprintf("%d", port),
		}},
		Volumes: []composetypes.ServiceVolumeConfig{{
			Type:   composetypes.VolumeTypeBind,
			Source: agentConfigDir,
			Target: "/config",
		}},
	}, nil
}

func sanitizeVersion(version string) string {
	if version == "" {
		return ""
	}

	sanitized := strings.ReplaceAll(version, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")
	sanitized = strings.ReplaceAll(sanitized, "*", "-")
	sanitized = strings.ReplaceAll(sanitized, "?", "-")
	sanitized = strings.ReplaceAll(sanitized, "\"", "-")
	sanitized = strings.ReplaceAll(sanitized, "<", "-")
	sanitized = strings.ReplaceAll(sanitized, ">", "-")
	sanitized = strings.ReplaceAll(sanitized, "|", "-")
	sanitized = strings.Trim(sanitized, ". ")
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}
	return sanitized
}

func translateLocalAgentGatewayConfig(
	ctx context.Context,
	engine gateway.Engine,
	agentGatewayPort uint16,
	servers []*runtimetypes.MCPServer,
	agents []*runtimetypes.Agent,
) (*runtimetypes.AgentGatewayConfig, error) {
	desired, err := buildDesiredAgentGatewayConfig(agentGatewayPort, servers, agents)
	if err != nil {
		return nil, err
	}
	return engine.Render(ctx, desired)
}

func buildDesiredAgentGatewayConfig(agentGatewayPort uint16, servers []*runtimetypes.MCPServer, agents []*runtimetypes.Agent) (gateway.Config, error) {
	var targets []gateway.MCPTarget

	for _, server := range servers {
		targetName := localMCPServiceName(server)
		mcpTarget := gateway.MCPTarget{Name: targetName}

		switch server.MCPServerType {
		case runtimetypes.MCPServerTypeRemote:
			mcpTarget.MCP = &gateway.MCPTargetSpec{
				Host: runtimeutils.BuildRemoteMCPURL(server.Remote),
			}
		case runtimetypes.MCPServerTypeLocal:
			switch server.Local.TransportType {
			case runtimetypes.TransportTypeStdio:
				if canRunInsideLocalAgentGateway(server.Local.Deployment.Cmd) {
					mcpTarget.Stdio = &gateway.StdioTargetSpec{
						Cmd:  server.Local.Deployment.Cmd,
						Args: server.Local.Deployment.Args,
						Env:  server.Local.Deployment.Env,
					}
				} else {
					mcpTarget.MCP = &gateway.MCPTargetSpec{
						Host: fmt.Sprintf("http://%s:%d/mcp", targetName, localOCIServerPort),
					}
				}
			case runtimetypes.TransportTypeHTTP:
				httpTransportConfig := server.Local.HTTP
				if httpTransportConfig == nil || httpTransportConfig.Port == 0 {
					return gateway.Config{}, fmt.Errorf("HTTP transport requires a target port")
				}
				mcpTarget.SSE = &gateway.SSETargetSpec{
					Host: targetName,
					Port: httpTransportConfig.Port,
					Path: httpTransportConfig.Path,
				}
			default:
				return gateway.Config{}, fmt.Errorf("unsupported transport type: %s", server.Local.TransportType)
			}
		}

		targets = append(targets, mcpTarget)
	}

	var routes []gateway.Route
	if len(targets) > 0 {
		routes = append(routes, gateway.Route{
			Name:       gateway.MCPRouteName,
			PathPrefix: "/mcp",
			MCP:        &gateway.MCPBackend{Targets: targets},
		})
	}

	var backends []gateway.Backend
	var policies []gateway.Policy
	if len(agents) > 0 {
		policies = append(policies, gateway.Policy{
			Name: agentRoutePolicyName,
			Type: "AgentRoute",
			Spec: gateway.PolicySpec{
				A2A:        &gateway.A2APolicy{},
				URLRewrite: &gateway.URLRewritePolicy{PathPrefix: "/"},
			},
		})
	}
	for _, agent := range agents {
		agentServiceName := localAgentServiceName(agent)
		backendName := fmt.Sprintf("%s_backend", agentServiceName)
		backends = append(backends, gateway.Backend{
			Name: backendName,
			URL:  fmt.Sprintf("%s:%d", agentServiceName, defaultAgentPort(agent)),
		})
		routes = append(routes, gateway.Route{
			Name:        fmt.Sprintf("%s_route", agentServiceName),
			PathPrefix:  fmt.Sprintf("/agents/%s", agentServiceName),
			BackendRefs: []gateway.BackendRef{{Name: backendName}},
			Policies:    []gateway.PolicyRef{{Name: agentRoutePolicyName}},
		})
	}

	return gateway.Config{
		ClassName: "agentgateway",
		Listeners: []gateway.Listener{{
			Name:     "default",
			Protocol: string(runtimetypes.LocalListenerProtocolHTTP),
			Port:     int(agentGatewayPort),
		}},
		Routes:   routes,
		Backends: backends,
		Policies: policies,
	}, nil
}

func defaultAgentPort(agent *runtimetypes.Agent) uint16 {
	if agent == nil || agent.Deployment.Port == 0 {
		return runtimeutils.DefaultLocalAgentPort
	}
	return agent.Deployment.Port
}

func extractServiceNames(config *runtimetypes.LocalRuntimeConfig) []string {
	if config == nil || config.DockerCompose == nil {
		return nil
	}
	names := make([]string, 0, len(config.DockerCompose.Services))
	for name := range config.DockerCompose.Services {
		if name == "agent_gateway" {
			continue
		}
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
