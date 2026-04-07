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

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	platformutils "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"go.yaml.in/yaml/v3"
)

const (
	localMCPRouteName         = "mcp_route"
	localComposeFileName      = "docker-compose.yaml"
	localAgentGatewayFileName = "agent-gateway.yaml"
	defaultLocalProjectName   = "agentregistry_runtime"
	localOCIServerPort        = 3000
)

func BuildLocalPlatformConfig(
	ctx context.Context,
	platformDir string,
	agentGatewayPort uint16,
	projectName string,
	desired *platformtypes.DesiredState,
) (*platformtypes.LocalPlatformConfig, error) {
	_ = ctx
	if strings.TrimSpace(projectName) == "" {
		projectName = defaultLocalProjectName
	}

	agentGatewayService, err := translateLocalAgentGatewayService(platformDir, agentGatewayPort)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway service: %w", err)
	}

	dockerComposeServices := map[string]composetypes.ServiceConfig{
		"agent_gateway": *agentGatewayService,
	}

	for _, mcpServer := range desired.MCPServers {
		if mcpServer.MCPServerType != platformtypes.MCPServerTypeLocal {
			continue
		}
		if mcpServer.Local.TransportType == platformtypes.TransportTypeStdio && canRunInsideLocalAgentGateway(mcpServer.Local.Deployment.Cmd) {
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

		serviceConfig, err := translateLocalAgentToServiceConfig(platformDir, agent)
		if err != nil {
			return nil, fmt.Errorf("failed to translate Agent %s to service config: %w", agent.Name, err)
		}
		dockerComposeServices[serviceName] = *serviceConfig
	}

	dockerCompose := &platformtypes.DockerComposeConfig{
		Name:       projectName,
		WorkingDir: platformDir,
		Services:   dockerComposeServices,
	}

	gatewayConfig, err := translateLocalAgentGatewayConfig(agentGatewayPort, desired.MCPServers, desired.Agents)
	if err != nil {
		return nil, fmt.Errorf("failed to translate agent gateway config: %w", err)
	}

	return &platformtypes.LocalPlatformConfig{
		DockerCompose: dockerCompose,
		AgentGateway:  gatewayConfig,
	}, nil
}

func WriteLocalPlatformFiles(platformDir string, cfg *platformtypes.LocalPlatformConfig, port uint16) error {
	if cfg == nil {
		return nil
	}
	if err := writeLocalDockerComposeConfig(platformDir, cfg.DockerCompose); err != nil {
		return err
	}
	if err := writeLocalAgentGatewayConfig(platformDir, cfg.AgentGateway, port); err != nil {
		return err
	}
	return nil
}

func ComposeUpLocalPlatform(ctx context.Context, platformDir string, verbose bool) error {
	if err := os.MkdirAll(platformDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "--remove-orphans", "--force-recreate")
	cmd.Dir = platformDir
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

func ComposeDownLocalPlatform(ctx context.Context, platformDir string, verbose bool) error {
	if _, err := os.Stat(platformDir); os.IsNotExist(err) {
		return nil
	}
	cmd := exec.CommandContext(ctx, "docker", "compose", "down", "--remove-orphans")
	cmd.Dir = platformDir
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

func LoadLocalDockerComposeConfig(platformDir string) (*platformtypes.DockerComposeConfig, error) {
	path := filepath.Join(platformDir, localComposeFileName)
	project := &platformtypes.DockerComposeConfig{
		Name:       defaultLocalProjectName,
		WorkingDir: platformDir,
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
		project.WorkingDir = platformDir
	}
	if project.Services == nil {
		project.Services = map[string]composetypes.ServiceConfig{}
	}
	return project, nil
}

func LoadLocalAgentGatewayConfig(platformDir string, port uint16) (*platformtypes.AgentGatewayConfig, error) {
	path := filepath.Join(platformDir, localAgentGatewayFileName)
	cfg := defaultLocalAgentGatewayConfig(port)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read agent gateway config: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("unmarshal agent gateway config: %w", err)
	}
	ensureLocalAgentGatewayDefaults(cfg, port)
	return cfg, nil
}

func writeLocalDockerComposeConfig(platformDir string, project *platformtypes.DockerComposeConfig) error {
	if err := os.MkdirAll(platformDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if project == nil {
		project = &platformtypes.DockerComposeConfig{
			Name:       defaultLocalProjectName,
			WorkingDir: platformDir,
			Services:   map[string]composetypes.ServiceConfig{},
		}
	}
	if project.Name == "" {
		project.Name = defaultLocalProjectName
	}
	if project.WorkingDir == "" {
		project.WorkingDir = platformDir
	}
	if project.Services == nil {
		project.Services = map[string]composetypes.ServiceConfig{}
	}
	content, err := project.MarshalYAML()
	if err != nil {
		return fmt.Errorf("marshal docker compose config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(platformDir, localComposeFileName), content, 0644); err != nil {
		return fmt.Errorf("write docker compose config: %w", err)
	}
	return nil
}

func writeLocalAgentGatewayConfig(platformDir string, cfg *platformtypes.AgentGatewayConfig, port uint16) error {
	if err := os.MkdirAll(platformDir, 0755); err != nil {
		return fmt.Errorf("create runtime directory: %w", err)
	}
	if cfg == nil {
		cfg = defaultLocalAgentGatewayConfig(port)
	}
	ensureLocalAgentGatewayDefaults(cfg, port)
	content, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal agent gateway config: %w", err)
	}
	if err := os.WriteFile(filepath.Join(platformDir, localAgentGatewayFileName), content, 0644); err != nil {
		return fmt.Errorf("write agent gateway config: %w", err)
	}
	return nil
}

func defaultLocalAgentGatewayConfig(port uint16) *platformtypes.AgentGatewayConfig {
	return &platformtypes.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []platformtypes.LocalBind{{
			Port: port,
			Listeners: []platformtypes.LocalListener{{
				Name:     "default",
				Protocol: platformtypes.LocalListenerProtocolHTTP,
				Routes:   []platformtypes.LocalRoute{},
			}},
		}},
	}
}

func ensureLocalAgentGatewayDefaults(cfg *platformtypes.AgentGatewayConfig, port uint16) {
	if cfg.Config == nil {
		cfg.Config = struct{}{}
	}
	if len(cfg.Binds) == 0 {
		cfg.Binds = defaultLocalAgentGatewayConfig(port).Binds
		return
	}
	if cfg.Binds[0].Port == 0 {
		cfg.Binds[0].Port = port
	}
	if len(cfg.Binds[0].Listeners) == 0 {
		cfg.Binds[0].Listeners = []platformtypes.LocalListener{{
			Name:     "default",
			Protocol: platformtypes.LocalListenerProtocolHTTP,
			Routes:   []platformtypes.LocalRoute{},
		}}
		return
	}
	if cfg.Binds[0].Listeners[0].Protocol == "" {
		cfg.Binds[0].Listeners[0].Protocol = platformtypes.LocalListenerProtocolHTTP
	}
}

func canRunInsideLocalAgentGateway(cmd string) bool {
	return cmd == "npx" || cmd == "uvx"
}

func localMCPServiceName(server *platformtypes.MCPServer) string {
	return platformutils.GenerateInternalNameForDeployment(server.Name, server.DeploymentID)
}

func localAgentServiceName(agent *platformtypes.Agent) string {
	return platformutils.GenerateInternalNameForDeployment(agent.Name, agent.DeploymentID)
}

func translateLocalAgentGatewayService(platformDir string, port uint16) (*composetypes.ServiceConfig, error) {
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
			Source: platformDir,
			Target: "/config",
		}},
	}, nil
}

func translateLocalMCPServerToServiceConfig(server *platformtypes.MCPServer) (*composetypes.ServiceConfig, error) {
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
	if server.Local.TransportType == platformtypes.TransportTypeStdio && !canRunInsideLocalAgentGateway(server.Local.Deployment.Cmd) {
		envValues = append(envValues, "HOST=0.0.0.0")
		envValues = append(envValues, "MCP_TRANSPORT_MODE=http")
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

func translateLocalAgentToServiceConfig(platformDir string, agent *platformtypes.Agent) (*composetypes.ServiceConfig, error) {
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
		port = platformutils.DefaultLocalAgentPort
	}

	var agentConfigDir string
	if agent.Version != "" {
		sanitizedVersion := utils.SanitizeVersion(agent.Version)
		agentConfigDir = filepath.Join(platformDir, agent.Name, sanitizedVersion)
	} else {
		agentConfigDir = filepath.Join(platformDir, agent.Name)
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

func translateLocalAgentGatewayConfig(agentGatewayPort uint16, servers []*platformtypes.MCPServer, agents []*platformtypes.Agent) (*platformtypes.AgentGatewayConfig, error) {
	var targets []platformtypes.MCPTarget

	for _, server := range servers {
		targetName := localMCPServiceName(server)
		mcpTarget := platformtypes.MCPTarget{Name: targetName}

		switch server.MCPServerType {
		case platformtypes.MCPServerTypeRemote:
			mcpTarget.MCP = &platformtypes.MCPTargetSpec{
				Host: platformutils.BuildRemoteMCPURL(server.Remote),
			}
		case platformtypes.MCPServerTypeLocal:
			switch server.Local.TransportType {
			case platformtypes.TransportTypeStdio:
				if canRunInsideLocalAgentGateway(server.Local.Deployment.Cmd) {
					mcpTarget.Stdio = &platformtypes.StdioTargetSpec{
						Cmd:  server.Local.Deployment.Cmd,
						Args: server.Local.Deployment.Args,
						Env:  server.Local.Deployment.Env,
					}
				} else {
					mcpTarget.MCP = &platformtypes.MCPTargetSpec{
						Host: fmt.Sprintf("http://%s:%d/mcp", targetName, localOCIServerPort),
					}
				}
			case platformtypes.TransportTypeHTTP:
				httpTransportConfig := server.Local.HTTP
				if httpTransportConfig == nil || httpTransportConfig.Port == 0 {
					return nil, fmt.Errorf("HTTP transport requires a target port")
				}
				mcpTarget.SSE = &platformtypes.SSETargetSpec{
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

	var agentRoutes []platformtypes.LocalRoute
	for _, agent := range agents {
		agentServiceName := localAgentServiceName(agent)
		route := platformtypes.LocalRoute{
			RouteName: fmt.Sprintf("%s_route", agentServiceName),
			Matches: []platformtypes.RouteMatch{{
				Path: platformtypes.PathMatch{
					PathPrefix: fmt.Sprintf("/agents/%s", agentServiceName),
				},
			}},
			Backends: []platformtypes.RouteBackend{{
				Weight: 100,
				Host:   fmt.Sprintf("%s:%d", agentServiceName, defaultAgentPort(agent)),
			}},
			Policies: &platformtypes.FilterOrPolicy{
				A2A: &platformtypes.A2APolicy{},
				URLRewrite: &platformtypes.URLRewrite{
					Path: &platformtypes.PathRedirect{Prefix: "/"},
				},
			},
		}
		agentRoutes = append(agentRoutes, route)
	}

	slices.SortStableFunc(agentRoutes, func(a, b platformtypes.LocalRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})
	slices.SortStableFunc(targets, func(a, b platformtypes.MCPTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})

	mcpRoute := platformtypes.LocalRoute{
		RouteName: localMCPRouteName,
		Matches: []platformtypes.RouteMatch{{
			Path: platformtypes.PathMatch{PathPrefix: "/mcp"},
		}},
		Backends: []platformtypes.RouteBackend{{
			Weight: 100,
			MCP: &platformtypes.MCPBackend{
				Targets: targets,
			},
		}},
	}

	var allRoutes []platformtypes.LocalRoute
	if len(targets) > 0 {
		allRoutes = append([]platformtypes.LocalRoute{}, mcpRoute)
	}
	allRoutes = append(allRoutes, agentRoutes...)

	return &platformtypes.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []platformtypes.LocalBind{{
			Port: agentGatewayPort,
			Listeners: []platformtypes.LocalListener{{
				Name:     "default",
				Protocol: platformtypes.LocalListenerProtocolHTTP,
				Routes:   allRoutes,
			}},
		}},
	}, nil
}

func defaultAgentPort(agent *platformtypes.Agent) uint16 {
	if agent == nil || agent.Deployment.Port == 0 {
		return platformutils.DefaultLocalAgentPort
	}
	return agent.Deployment.Port
}

func mustAgentManifest(
	ctx context.Context,
	agentService agentsvc.Registry,
	deployment *models.Deployment,
) *models.AgentManifest {
	agentResp, err := agentService.GetAgentVersion(ctx, deployment.ServerName, deployment.Version)
	if err != nil {
		return nil
	}
	manifestCopy := agentResp.Agent.AgentManifest
	return &manifestCopy
}

func extractServiceNames(config *platformtypes.LocalPlatformConfig) []string {
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

func extractTargetNames(config *platformtypes.AgentGatewayConfig) []string {
	targets := extractMCPRouteTargets(config)
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, target.Name)
	}
	slices.Sort(names)
	return names
}

func extractNonMCPRouteNames(config *platformtypes.AgentGatewayConfig) []string {
	routes := extractNonMCPRoutes(config)
	names := make([]string, 0, len(routes))
	for _, route := range routes {
		names = append(names, route.RouteName)
	}
	slices.Sort(names)
	return names
}

func extractNonMCPRoutes(config *platformtypes.AgentGatewayConfig) []platformtypes.LocalRoute {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	var routes []platformtypes.LocalRoute
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName == localMCPRouteName {
			continue
		}
		routes = append(routes, route)
	}
	return routes
}

func extractMCPRouteTargets(config *platformtypes.AgentGatewayConfig) []platformtypes.MCPTarget {
	if config == nil || len(config.Binds) == 0 || len(config.Binds[0].Listeners) == 0 {
		return nil
	}
	for _, route := range config.Binds[0].Listeners[0].Routes {
		if route.RouteName != localMCPRouteName {
			continue
		}
		if len(route.Backends) == 0 || route.Backends[0].MCP == nil {
			return nil
		}
		return append([]platformtypes.MCPTarget{}, route.Backends[0].MCP.Targets...)
	}
	return nil
}

func mergeAgentGatewayConfig(
	existing *platformtypes.AgentGatewayConfig,
	incoming *platformtypes.AgentGatewayConfig,
	targetNames []string,
	routeNames []string,
	remove bool,
	port uint16,
) {
	ensureLocalAgentGatewayDefaults(existing, port)
	if incoming == nil || len(existing.Binds) == 0 || len(existing.Binds[0].Listeners) == 0 {
		return
	}

	listener := &existing.Binds[0].Listeners[0]
	listener.Routes = filterRoutes(listener.Routes, routeNames)

	targetSet := make(map[string]struct{}, len(targetNames))
	for _, name := range targetNames {
		targetSet[name] = struct{}{}
	}

	var existingTargets []platformtypes.MCPTarget
	var otherRoutes []platformtypes.LocalRoute
	for _, route := range listener.Routes {
		if route.RouteName == localMCPRouteName {
			if len(route.Backends) > 0 && route.Backends[0].MCP != nil {
				for _, target := range route.Backends[0].MCP.Targets {
					if _, shouldRemove := targetSet[target.Name]; !shouldRemove {
						existingTargets = append(existingTargets, target)
					}
				}
			}
			continue
		}
		otherRoutes = append(otherRoutes, route)
	}

	if !remove {
		existingTargets = append(existingTargets, extractMCPRouteTargets(incoming)...)
		otherRoutes = append(otherRoutes, extractNonMCPRoutes(incoming)...)
	}

	slices.SortFunc(existingTargets, func(a, b platformtypes.MCPTarget) int {
		return cmp.Compare(a.Name, b.Name)
	})
	slices.SortFunc(otherRoutes, func(a, b platformtypes.LocalRoute) int {
		return cmp.Compare(a.RouteName, b.RouteName)
	})

	routes := make([]platformtypes.LocalRoute, 0, len(otherRoutes)+1)
	if len(existingTargets) > 0 {
		routes = append(routes, platformtypes.LocalRoute{
			RouteName: localMCPRouteName,
			Matches: []platformtypes.RouteMatch{{
				Path: platformtypes.PathMatch{PathPrefix: "/mcp"},
			}},
			Backends: []platformtypes.RouteBackend{{
				Weight: 100,
				MCP:    &platformtypes.MCPBackend{Targets: existingTargets},
			}},
		})
	}
	routes = append(routes, otherRoutes...)
	listener.Routes = routes
}

func filterRoutes(routes []platformtypes.LocalRoute, names []string) []platformtypes.LocalRoute {
	if len(names) == 0 {
		return routes
	}
	nameSet := make(map[string]struct{}, len(names))
	for _, name := range names {
		nameSet[name] = struct{}{}
	}
	filtered := make([]platformtypes.LocalRoute, 0, len(routes))
	for _, route := range routes {
		if _, remove := nameSet[route.RouteName]; remove {
			continue
		}
		filtered = append(filtered, route)
	}
	return filtered
}
