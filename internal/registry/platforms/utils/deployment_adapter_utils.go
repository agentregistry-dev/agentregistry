package utils

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

const DefaultLocalAgentPort uint16 = 8080

type MCPServerRunRequest struct {
	RegistryServer *apiv0.ServerJSON
	DeploymentID   string
	PreferRemote   bool
	EnvValues      map[string]string
	ArgValues      map[string]string
	HeaderValues   map[string]string
}

func TranslateMCPServer(ctx context.Context, req *MCPServerRunRequest) (*platformtypes.MCPServer, error) {
	if req == nil || req.RegistryServer == nil {
		return nil, fmt.Errorf("registry server is required")
	}
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
) (*platformtypes.MCPServer, error) {
	remoteInfo := registryServer.Remotes[0]

	headersMap, err := processHeaders(remoteInfo.Headers, headerValues)
	if err != nil {
		return nil, err
	}

	headers := make([]platformtypes.HeaderValue, 0, len(headersMap))
	for k, v := range headersMap {
		headers = append(headers, platformtypes.HeaderValue{
			Name:  k,
			Value: v,
		})
	}

	u, err := parseURL(remoteInfo.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote server url: %v", err)
	}

	return &platformtypes.MCPServer{
		Name:          generateInternalName(registryServer.Name),
		DeploymentID:  deploymentID,
		MCPServerType: platformtypes.MCPServerTypeRemote,
		Remote: &platformtypes.RemoteMCPServer{
			Scheme:  u.scheme,
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
) (*platformtypes.MCPServer, error) {
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

	config, args, err := GetRegistryConfig(packageInfo, args)
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
		transportType platformtypes.TransportType
		httpTransport *platformtypes.HTTPTransport
	)
	switch packageInfo.Transport.Type {
	case "stdio":
		transportType = platformtypes.TransportTypeStdio
	default:
		transportType = platformtypes.TransportTypeHTTP
		u, err := parseURL(packageInfo.Transport.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse transport url: %v", err)
		}
		httpTransport = &platformtypes.HTTPTransport{
			Port: u.port,
			Path: u.path,
		}
	}

	return &platformtypes.MCPServer{
		Name:          generateInternalName(registryServer.Name),
		DeploymentID:  deploymentID,
		MCPServerType: platformtypes.MCPServerTypeLocal,
		Local: &platformtypes.LocalMCPServer{
			Deployment: platformtypes.MCPServerDeployment{
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
	scheme string
	host   string
	port   uint32
	path   string
}

func parseURL(rawURL string) (*parsedURL, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse server remote url: %v", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q: only http and https are supported", u.Scheme)
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
		scheme: u.Scheme,
		host:   u.Hostname(),
		port:   port,
		path:   u.Path,
	}, nil
}

// BuildRemoteMCPURL constructs a well-formed URL from a RemoteMCPServer,
// handling IPv6 bracketing and standard-port omission.
func BuildRemoteMCPURL(remote *platformtypes.RemoteMCPServer) string {
	scheme := remote.Scheme
	if scheme == "" {
		scheme = "http"
	}

	var host string
	includePort := (scheme == "https" && remote.Port != 443) || (scheme == "http" && remote.Port != 80)
	if includePort {
		host = net.JoinHostPort(remote.Host, fmt.Sprintf("%d", remote.Port))
	} else if strings.Contains(remote.Host, ":") {
		host = "[" + remote.Host + "]"
	} else {
		host = remote.Host
	}

	u := &url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   remote.Path,
	}
	return u.String()
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

type RegistryConfig struct {
	Image   string
	Command string
	IsOCI   bool
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
	slices.Sort(result)
	return result
}
