package utils

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"

	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
)

func ValidateDeploymentRequest(deployment *models.Deployment, allowExisting bool) error {
	if deployment == nil {
		return fmt.Errorf("deployment request is required: %w", database.ErrInvalidInput)
	}
	if strings.TrimSpace(deployment.ProviderID) == "" {
		return fmt.Errorf("provider id is required: %w", database.ErrInvalidInput)
	}
	if len(deployment.ProviderConfig) > 0 {
		return fmt.Errorf("providerConfig is not supported for OSS adapters: %w", database.ErrInvalidInput)
	}
	if allowExisting {
		if strings.TrimSpace(deployment.ID) == "" {
			return fmt.Errorf("deployment id is required: %w", database.ErrInvalidInput)
		}
	}
	return nil
}

func BuildPlatformMCPServer(
	ctx context.Context,
	registryService service.RegistryService,
	deployment *models.Deployment,
	namespace string,
) (*platformtypes.MCPServer, error) {
	serverResp, err := registryService.GetServerByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
	if err != nil {
		return nil, fmt.Errorf("load mcp server %s@%s: %w", deployment.ServerName, deployment.Version, err)
	}
	envValues, argValues, headerValues := splitDeploymentRuntimeInputs(deployment.Env)
	server, err := translateMCPServer(ctx, &mcpServerRunRequest{
		registryServer: &serverResp.Server,
		deploymentID:   deployment.ID,
		preferRemote:   deployment.PreferRemote,
		envValues:      envValues,
		argValues:      argValues,
		headerValues:   headerValues,
	})
	if err != nil {
		return nil, err
	}
	if namespace != "" && server.Namespace == "" {
		server.Namespace = namespace
	}
	return server, nil
}

func ResolveAgent(
	ctx context.Context,
	registryService service.RegistryService,
	deployment *models.Deployment,
	namespace string,
) (*platformtypes.ResolvedAgentConfig, error) {
	agentResp, err := registryService.GetAgentByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
	if err != nil {
		return nil, fmt.Errorf("load agent %s@%s: %w", deployment.ServerName, deployment.Version, err)
	}
	envValues := copyStringMap(deployment.Env)
	if envValues["KAGENT_NAMESPACE"] == "" {
		envValues["KAGENT_NAMESPACE"] = namespace
	}
	envValues["KAGENT_URL"] = "http://localhost"
	envValues["KAGENT_NAME"] = agentResp.Agent.AgentManifest.Name
	envValues["AGENT_NAME"] = agentResp.Agent.AgentManifest.Name
	envValues["MODEL_PROVIDER"] = agentResp.Agent.AgentManifest.ModelProvider
	envValues["MODEL_NAME"] = agentResp.Agent.AgentManifest.ModelName

	resolvedServers, resolvedConfigs, _, err := resolveAgentManifestPlatformMCPServers(ctx, registryService, deployment.ID, &agentResp.Agent.AgentManifest, namespace)
	if err != nil {
		return nil, err
	}
	skills, err := registryService.ResolveAgentManifestSkills(ctx, &agentResp.Agent.AgentManifest)
	if err != nil {
		return nil, err
	}

	return &platformtypes.ResolvedAgentConfig{
		Agent: &platformtypes.Agent{
			Name:               agentResp.Agent.Name,
			Version:            agentResp.Agent.Version,
			DeploymentID:       deployment.ID,
			Deployment:         platformtypes.AgentDeployment{Image: agentResp.Agent.Image, Env: envValues},
			ResolvedMCPServers: resolvedConfigs,
			Skills:             skills,
		},
		ResolvedPlatformServers: resolvedServers,
		ResolvedConfigServers:   resolvedConfigs,
	}, nil
}

func resolveAgentManifestPlatformMCPServers(
	ctx context.Context,
	registryService service.RegistryService,
	deploymentID string,
	manifest *models.AgentManifest,
	namespace string,
) ([]*platformtypes.MCPServer, []platformtypes.ResolvedMCPServerConfig, []common.PythonMCPServer, error) {
	if manifest == nil {
		return nil, nil, nil, nil
	}

	var platformServers []*platformtypes.MCPServer
	var configServers []platformtypes.ResolvedMCPServerConfig
	var pythonServers []common.PythonMCPServer

	for _, mcpServer := range manifest.McpServers {
		if mcpServer.Type != "registry" {
			continue
		}

		version := strings.TrimSpace(mcpServer.RegistryServerVersion)
		if version == "" {
			version = "latest"
		}

		serverResp, err := registryService.GetServerByNameAndVersion(ctx, mcpServer.RegistryServerName, version)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load resolved MCP server %s@%s: %w", mcpServer.RegistryServerName, version, err)
		}

		platformServer, err := translateMCPServer(ctx, &mcpServerRunRequest{
			registryServer: &serverResp.Server,
			deploymentID:   deploymentID,
			preferRemote:   mcpServer.RegistryServerPreferRemote,
			envValues:      map[string]string{},
			argValues:      map[string]string{},
			headerValues:   map[string]string{},
		})
		if err != nil {
			return nil, nil, nil, err
		}
		if namespace != "" && platformServer.Namespace == "" {
			platformServer.Namespace = namespace
		}
		platformServers = append(platformServers, platformServer)

		configServer := resolvedMCPConfigFromRegistryServer(&serverResp.Server, deploymentID, mcpServer.RegistryServerPreferRemote)
		configServers = append(configServers, configServer)
		pythonServers = append(pythonServers, common.PythonMCPServer{
			Name:    configServer.Name,
			Type:    configServer.Type,
			URL:     configServer.URL,
			Headers: configServer.Headers,
		})
	}

	return platformServers, configServers, pythonServers, nil
}

func resolvedMCPConfigFromRegistryServer(
	server *apiv0.ServerJSON,
	deploymentID string,
	preferRemote bool,
) platformtypes.ResolvedMCPServerConfig {
	if server == nil {
		return platformtypes.ResolvedMCPServerConfig{
			Name: GenerateInternalNameForDeployment("", deploymentID),
			Type: "command",
		}
	}
	cfg := platformtypes.ResolvedMCPServerConfig{
		Name: GenerateInternalNameForDeployment(server.Name, deploymentID),
		Type: "command",
	}
	useRemote := len(server.Remotes) > 0 && (preferRemote || len(server.Packages) == 0)
	if !useRemote {
		return cfg
	}
	cfg.Type = "remote"
	cfg.URL = server.Remotes[0].URL
	if len(server.Remotes[0].Headers) > 0 {
		headers := make(map[string]string, len(server.Remotes[0].Headers))
		for _, header := range server.Remotes[0].Headers {
			headers[header.Name] = header.Value
		}
		cfg.Headers = headers
	}
	return cfg
}

var ErrDeploymentNotSupported = errors.New("deployment operation is not supported for this provider platform type")

type mcpServerRunRequest struct {
	registryServer *apiv0.ServerJSON
	deploymentID   string
	preferRemote   bool
	envValues      map[string]string
	argValues      map[string]string
	headerValues   map[string]string
}

func translateMCPServer(
	ctx context.Context,
	req *mcpServerRunRequest,
) (*api.MCPServer, error) {
	useRemote := len(req.registryServer.Remotes) > 0 && (req.preferRemote || len(req.registryServer.Packages) == 0)
	usePackage := len(req.registryServer.Packages) > 0 && (!req.preferRemote || len(req.registryServer.Remotes) == 0)

	switch {
	case useRemote:
		return translateRemoteMCPServer(
			ctx,
			req.registryServer,
			req.deploymentID,
			req.headerValues,
		)
	case usePackage:
		return translateLocalMCPServer(
			ctx,
			req.registryServer,
			req.deploymentID,
			req.envValues,
			req.argValues,
		)
	}

	return nil, fmt.Errorf("no valid deployment method found for server: %s", req.registryServer.Name)
}

func translateRemoteMCPServer(
	ctx context.Context,
	registryServer *apiv0.ServerJSON,
	deploymentID string,
	headerValues map[string]string,
) (*api.MCPServer, error) {
	remoteInfo := registryServer.Remotes[0]

	headersMap, err := processHeaders(remoteInfo.Headers, headerValues)
	if err != nil {
		return nil, err
	}

	headers := make([]api.HeaderValue, 0, len(headersMap))
	for k, v := range headersMap {
		headers = append(headers, api.HeaderValue{
			Name:  k,
			Value: v,
		})
	}

	u, err := parseURL(remoteInfo.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote server url: %v", err)
	}

	return &api.MCPServer{
		Name:          generateInternalName(registryServer.Name),
		DeploymentID:  deploymentID,
		MCPServerType: api.MCPServerTypeRemote,
		Remote: &api.RemoteMCPServer{
			Host:    u.host,
			Port:    u.port,
			Path:    u.path,
			Headers: headers,
		},
	}, nil
}

func translateLocalMCPServer(
	ctx context.Context,
	registryServer *apiv0.ServerJSON,
	deploymentID string,
	envValues map[string]string,
	argValues map[string]string,
) (*api.MCPServer, error) {
	packageInfo := registryServer.Packages[0]

	var args []string
	processedArgs := make(map[string]bool)
	addProcessedArgs := func(modelArgs []model.Argument) {
		for _, arg := range modelArgs {
			processedArgs[arg.Name] = true
		}
	}

	args = processArguments(args, packageInfo.RuntimeArguments, argValues)
	addProcessedArgs(packageInfo.RuntimeArguments)

	config, args, err := getRegistryConfig(packageInfo, args)
	if err != nil {
		return nil, err
	}

	args = processArguments(args, packageInfo.PackageArguments, argValues)
	addProcessedArgs(packageInfo.PackageArguments)

	var extraArgNames []string
	for argName := range argValues {
		if !processedArgs[argName] {
			extraArgNames = append(extraArgNames, argName)
		}
	}
	slices.Sort(extraArgNames)
	for _, argName := range extraArgNames {
		args = append(args, argName)
		if argValue := argValues[argName]; argValue != "" {
			args = append(args, argValue)
		}
	}

	processedEnvVars, err := processEnvironmentVariables(packageInfo.EnvironmentVariables, envValues)
	if err != nil {
		return nil, err
	}
	for key, value := range processedEnvVars {
		if _, exists := envValues[key]; !exists {
			envValues[key] = value
		}
	}

	var (
		transportType api.TransportType
		httpTransport *api.HTTPTransport
	)
	switch packageInfo.Transport.Type {
	case "stdio":
		transportType = api.TransportTypeStdio
	default:
		transportType = api.TransportTypeHTTP
		u, err := parseURL(packageInfo.Transport.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse transport url: %v", err)
		}
		httpTransport = &api.HTTPTransport{
			Port: u.port,
			Path: u.path,
		}
	}

	return &api.MCPServer{
		Name:          generateInternalName(registryServer.Name),
		DeploymentID:  deploymentID,
		MCPServerType: api.MCPServerTypeLocal,
		Local: &api.LocalMCPServer{
			Deployment: api.MCPServerDeployment{
				Image: config.image,
				Cmd:   config.command,
				Args:  args,
				Env:   envValues,
			},
			TransportType: transportType,
			HTTP:          httpTransport,
		},
	}, nil
}

type parsedURL struct {
	host string
	port uint32
	path string
}

func parseURL(rawURL string) (*parsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server remote url: %v", err)
	}
	portStr := u.Port()
	var port uint32
	if portStr == "" {
		if u.Scheme == "https" {
			port = 443
		} else {
			port = 80
		}
	} else {
		portI, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse server remote url: %v", err)
		}
		port = uint32(portI)
	}

	return &parsedURL{
		host: u.Hostname(),
		port: port,
		path: u.Path,
	}, nil
}

func generateInternalName(server string) string {
	name := strings.ToLower(strings.ReplaceAll(server, " ", "-"))
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, "/", "-")
	name = strings.ReplaceAll(name, ":", "-")
	name = strings.ReplaceAll(name, "@", "-")
	name = strings.ReplaceAll(name, "#", "-")
	name = strings.ReplaceAll(name, "$", "-")
	name = strings.ReplaceAll(name, "%", "-")
	name = strings.ReplaceAll(name, "^", "-")
	name = strings.ReplaceAll(name, "&", "-")
	name = strings.ReplaceAll(name, "*", "-")
	name = strings.ReplaceAll(name, "(", "-")
	name = strings.ReplaceAll(name, ")", "-")
	name = strings.ReplaceAll(name, "[", "-")
	name = strings.ReplaceAll(name, "]", "-")
	name = strings.ReplaceAll(name, "{", "-")
	name = strings.ReplaceAll(name, "}", "-")
	name = strings.ReplaceAll(name, "|", "-")
	name = strings.ReplaceAll(name, "\\", "-")
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, ",", "-")
	name = strings.ReplaceAll(name, "!", "-")
	name = strings.ReplaceAll(name, "?", "-")
	name = strings.ReplaceAll(name, " ", "-")
	return name
}

func GenerateInternalNameForDeployment(name, deploymentID string) string {
	base := generateInternalName(name)
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, generateInternalName(deploymentID))
}

type registryConfig struct {
	image   string
	command string
	isOCI   bool
}

func processArguments(
	args []string,
	modelArgs []model.Argument,
	argOverrides map[string]string,
) []string {
	getArgValue := func(arg model.Argument) string {
		if argOverrides != nil {
			if v, exists := argOverrides[arg.Name]; exists {
				return v
			}
		}
		if arg.Value != "" {
			return arg.Value
		}
		return arg.Default
	}

	for _, arg := range modelArgs {
		if arg.Type == model.ArgumentTypePositional {
			value := getArgValue(arg)
			if value != "" {
				args = append(args, value)
			}
		}
	}
	for _, arg := range modelArgs {
		if arg.Type == model.ArgumentTypeNamed {
			args = append(args, arg.Name)
			value := getArgValue(arg)
			if value != "" {
				args = append(args, value)
			}
		}
	}
	return args
}

func processEnvironmentVariables(
	envVars []model.KeyValueInput,
	overrides map[string]string,
) (map[string]string, error) {
	result := make(map[string]string)
	var missingRequired []string

	for _, env := range envVars {
		var value string
		if override, exists := overrides[env.Name]; exists {
			value = override
		} else if env.Value != "" {
			value = env.Value
		} else if env.Default != "" {
			value = env.Default
		}
		if env.IsRequired && value == "" {
			missingRequired = append(missingRequired, env.Name)
		}
		if value != "" {
			result[env.Name] = value
		}
	}

	if len(missingRequired) > 0 {
		return nil, fmt.Errorf("missing required environment variables: %s", strings.Join(missingRequired, ", "))
	}

	for key, value := range overrides {
		found := false
		for _, env := range envVars {
			if env.Name == key {
				found = true
				break
			}
		}
		if !found {
			result[key] = value
		}
	}

	return result, nil
}

func processHeaders(
	headers []model.KeyValueInput,
	headerOverrides map[string]string,
) (map[string]string, error) {
	result := make(map[string]string)
	var missingRequired []string

	for _, h := range headers {
		var value string
		if headerOverrides != nil {
			if override, exists := headerOverrides[h.Name]; exists {
				value = override
			}
		}
		if value == "" {
			value = h.Value
		}
		if value == "" {
			value = h.Default
		}
		if h.IsRequired && value == "" {
			missingRequired = append(missingRequired, h.Name)
		}
		if value != "" {
			result[h.Name] = value
		}
	}

	if len(missingRequired) > 0 {
		return nil, fmt.Errorf("missing required headers: %s", strings.Join(missingRequired, ", "))
	}

	return result, nil
}

func getRegistryConfig(
	packageInfo model.Package,
	args []string,
) (registryConfig, []string, error) {
	var config registryConfig
	normalizedType := strings.ToLower(string(packageInfo.RegistryType))

	switch normalizedType {
	case strings.ToLower(string(model.RegistryTypeNPM)):
		config.image = "node:24-alpine3.21"
		config.command = packageInfo.RunTimeHint
		if config.command == "" {
			config.command = "npx"
		}
		if !slices.Contains(args, "-y") {
			args = append(args, "-y")
		}
		if packageInfo.Version != "" {
			args = append(args, packageInfo.Identifier+"@"+packageInfo.Version)
		} else {
			args = append(args, packageInfo.Identifier)
		}
	case strings.ToLower(string(model.RegistryTypePyPI)):
		config.image = "ghcr.io/astral-sh/uv:debian"
		config.command = packageInfo.RunTimeHint
		if config.command == "" {
			config.command = "uvx"
		}
		if packageInfo.Version != "" {
			args = append(args, packageInfo.Identifier+"=="+packageInfo.Version)
		} else {
			args = append(args, packageInfo.Identifier)
		}
	case strings.ToLower(string(model.RegistryTypeOCI)):
		config.image = packageInfo.Identifier
		config.command = packageInfo.RunTimeHint
		config.isOCI = true
	default:
		return registryConfig{}, nil, fmt.Errorf("unsupported package registry type: %s", string(packageInfo.RegistryType))
	}

	return config, args, nil
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	maps.Copy(out, input)
	return out
}

func splitDeploymentRuntimeInputs(input map[string]string) (map[string]string, map[string]string, map[string]string) {
	if len(input) == 0 {
		return map[string]string{}, map[string]string{}, map[string]string{}
	}
	envValues := make(map[string]string, len(input))
	argValues := map[string]string{}
	headerValues := map[string]string{}
	for key, value := range input {
		switch {
		case strings.HasPrefix(key, "ARG_"):
			name := strings.TrimPrefix(key, "ARG_")
			if name != "" {
				argValues[name] = value
			}
		case strings.HasPrefix(key, "HEADER_"):
			name := strings.TrimPrefix(key, "HEADER_")
			if name != "" {
				headerValues[name] = value
			}
		default:
			envValues[key] = value
		}
	}
	return envValues, argValues, headerValues
}
