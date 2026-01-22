package registry

import (
	"context"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	registryutils "github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry/utils"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/modelcontextprotocol/registry/pkg/model"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type MCPServerRunRequest struct {
	RegistryServer *apiv0.ServerJSON
	PreferRemote   bool
	EnvValues      map[string]string
	ArgValues      map[string]string
	HeaderValues   map[string]string
}

type AgentRunRequest struct {
	RegistryAgent *models.AgentJSON
	EnvValues     map[string]string
	// Registry-type MCP servers resolved from agent manifest at deploy time to inject into the agent
	ResolvedMCPServers []*MCPServerRunRequest
}

// Translator is the interface for translating MCPServer objects to AgentGateway objects.
type Translator interface {
	TranslateMCPServer(
		ctx context.Context,
		req *MCPServerRunRequest,
	) (*api.MCPServer, error)

	TranslateAgent(
		ctx context.Context,
		req *AgentRunRequest,
	) (*api.Agent, error)
}

type registryTranslator struct{}

func NewTranslator() Translator {
	return &registryTranslator{}
}

func (t *registryTranslator) TranslateAgent(
	ctx context.Context,
	req *AgentRunRequest,
) (*api.Agent, error) {
	manifest := &req.RegistryAgent.AgentManifest

	// Build environment variables map starting with passed values
	env := make(map[string]string)
	maps.Copy(env, req.EnvValues)

	// TODO: remove kagent variables (currently required)
	// note that the change to remove this would have to be done in kagent-adk
	env["KAGENT_URL"] = "http://localhost"
	env["KAGENT_NAME"] = manifest.Name
	env["KAGENT_NAMESPACE"] = "default"

	// Set agent configuration
	env["AGENT_NAME"] = manifest.Name
	env["MODEL_PROVIDER"] = manifest.ModelProvider
	env["MODEL_NAME"] = manifest.ModelName

	port, err := utils.FindAvailablePort()
	if err != nil {
		return nil, fmt.Errorf("failed to find available port: %w", err)
	}
	return &api.Agent{
		Name:    req.RegistryAgent.Name,
		Version: req.RegistryAgent.Version,
		Deployment: api.AgentDeployment{
			Image: req.RegistryAgent.Image,
			Port:  port,
			Env:   env,
		},
	}, nil
}

func (t *registryTranslator) TranslateMCPServer(
	ctx context.Context,
	req *MCPServerRunRequest,
) (*api.MCPServer, error) {
	useRemote := len(req.RegistryServer.Remotes) > 0 && (req.PreferRemote || len(req.RegistryServer.Packages) == 0)
	usePackage := len(req.RegistryServer.Packages) > 0 && (!req.PreferRemote || len(req.RegistryServer.Remotes) == 0)

	switch {
	case useRemote:
		return translateRemoteMCPServer(
			ctx,
			req.RegistryServer,
			req.HeaderValues,
		)
	case usePackage:
		return translateLocalMCPServer(
			ctx,
			req.RegistryServer,
			req.EnvValues,
			req.ArgValues,
		)
	}

	return nil, fmt.Errorf("no valid deployment method found for server: %s", req.RegistryServer.Name)
}

func translateRemoteMCPServer(
	ctx context.Context,
	registryServer *apiv0.ServerJSON,
	headerValues map[string]string,
) (*api.MCPServer, error) {
	remoteInfo := registryServer.Remotes[0]

	// Process headers
	headersMap, err := registryutils.ProcessHeaders(remoteInfo.Headers, headerValues)
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

	u, err := parseUrl(remoteInfo.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote server url: %v", err)
	}

	return &api.MCPServer{
		Name:          generateInternalName(registryServer.Name),
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
	envValues map[string]string,
	argValues map[string]string,
) (*api.MCPServer, error) {
	// deploy the server either as stdio or http
	packageInfo := registryServer.Packages[0]

	var args []string

	// Track which arguments have been processed from the spec
	processedArgs := make(map[string]bool)
	addProcessedArgs := func(modelArgs []model.Argument) {
		for _, arg := range modelArgs {
			processedArgs[arg.Name] = true
		}
	}

	// Process runtime arguments first
	args = registryutils.ProcessArguments(args, packageInfo.RuntimeArguments, argValues)
	addProcessedArgs(packageInfo.RuntimeArguments)

	// Determine image and command based on registry type
	config, args, err := registryutils.GetRegistryConfig(packageInfo, args)
	if err != nil {
		return nil, err
	}

	// Process package arguments after the package identifier
	args = registryutils.ProcessArguments(args, packageInfo.PackageArguments, argValues)
	addProcessedArgs(packageInfo.PackageArguments)

	// Add any extra args that weren't in the spec
	var extraArgNames []string
	for argName := range argValues {
		if !processedArgs[argName] {
			extraArgNames = append(extraArgNames, argName)
		}
	}
	slices.Sort(extraArgNames)
	for _, argName := range extraArgNames {
		args = append(args, argName)
		// Only add the value if it's not empty
		// This allows users to pass flags like --verbose= (empty value means flag only)
		if argValue := argValues[argName]; argValue != "" {
			args = append(args, argValue)
		}
	}

	// Process environment variables using shared utility
	// The function returns a map with all processed env vars, including defaults
	processedEnvVars, err := registryutils.ProcessEnvironmentVariables(packageInfo.EnvironmentVariables, envValues)
	if err != nil {
		return nil, err
	}

	// Merge processed env vars into envValues (existing values take precedence)
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
		u, err := parseUrl(packageInfo.Transport.URL)
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
		MCPServerType: api.MCPServerTypeLocal,
		Local: &api.LocalMCPServer{
			Deployment: api.MCPServerDeployment{
				Image: config.Image,
				Cmd:   config.Command,
				Args:  args,
				Env:   envValues,
			},
			TransportType: transportType,
			HTTP:          httpTransport,
		},
	}, nil
}

type parsedUrl struct {
	host string
	port uint32
	path string
}

func parseUrl(rawUrl string) (*parsedUrl, error) {
	u, err := url.Parse(rawUrl)
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

	return &parsedUrl{
		host: u.Hostname(),
		port: port,
		path: u.Path,
	}, nil
}

func generateInternalName(server string) string {
	// convert the server name to a dns-1123 compliant name
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
