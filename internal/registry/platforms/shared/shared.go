package shared

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"

	api "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/modelcontextprotocol/registry/pkg/model"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type MCPServerRunRequest struct {
	RegistryServer *apiv0.ServerJSON
	DeploymentID   string
	PreferRemote   bool
	EnvValues      map[string]string
	ArgValues      map[string]string
	HeaderValues   map[string]string
}

type Translator interface {
	TranslateMCPServer(
		ctx context.Context,
		req *MCPServerRunRequest,
	) (*api.MCPServer, error)
}

type translator struct{}

func NewTranslator() Translator {
	return &translator{}
}

func (t *translator) TranslateMCPServer(
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
			req.DeploymentID,
			req.HeaderValues,
		)
	case usePackage:
		return translateLocalMCPServer(
			ctx,
			req.RegistryServer,
			req.DeploymentID,
			req.EnvValues,
			req.ArgValues,
		)
	}

	return nil, fmt.Errorf("no valid deployment method found for server: %s", req.RegistryServer.Name)
}

func translateRemoteMCPServer(
	ctx context.Context,
	registryServer *apiv0.ServerJSON,
	deploymentID string,
	headerValues map[string]string,
) (*api.MCPServer, error) {
	remoteInfo := registryServer.Remotes[0]

	headersMap, err := ProcessHeaders(remoteInfo.Headers, headerValues)
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
		Name:          GenerateInternalName(registryServer.Name),
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

	args = ProcessArguments(args, packageInfo.RuntimeArguments, argValues)
	addProcessedArgs(packageInfo.RuntimeArguments)

	config, args, err := GetRegistryConfig(packageInfo, args)
	if err != nil {
		return nil, err
	}

	args = ProcessArguments(args, packageInfo.PackageArguments, argValues)
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

	processedEnvVars, err := ProcessEnvironmentVariables(packageInfo.EnvironmentVariables, envValues)
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
		Name:          GenerateInternalName(registryServer.Name),
		DeploymentID:  deploymentID,
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

func GenerateInternalName(server string) string {
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
	base := GenerateInternalName(name)
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, GenerateInternalName(deploymentID))
}

type RegistryConfig struct {
	Image   string
	Command string
	IsOCI   bool
}

func ProcessArguments(
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

func ProcessEnvironmentVariables(
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

func ProcessHeaders(
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

func GetRegistryConfig(
	packageInfo model.Package,
	args []string,
) (RegistryConfig, []string, error) {
	var config RegistryConfig
	normalizedType := strings.ToLower(string(packageInfo.RegistryType))

	switch normalizedType {
	case strings.ToLower(string(model.RegistryTypeNPM)):
		config.Image = "node:24-alpine3.21"
		config.Command = packageInfo.RunTimeHint
		if config.Command == "" {
			config.Command = "npx"
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
		config.Image = "ghcr.io/astral-sh/uv:debian"
		config.Command = packageInfo.RunTimeHint
		if config.Command == "" {
			config.Command = "uvx"
		}
		if packageInfo.Version != "" {
			args = append(args, packageInfo.Identifier+"=="+packageInfo.Version)
		} else {
			args = append(args, packageInfo.Identifier)
		}
	case strings.ToLower(string(model.RegistryTypeOCI)):
		config.Image = packageInfo.Identifier
		config.Command = packageInfo.RunTimeHint
		config.IsOCI = true
	default:
		return RegistryConfig{}, nil, fmt.Errorf("unsupported package registry type: %s", string(packageInfo.RegistryType))
	}

	return config, args, nil
}

func EnvMapToStringSlice(envMap map[string]string) []string {
	result := make([]string, 0, len(envMap))
	for key, value := range envMap {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	return result
}
