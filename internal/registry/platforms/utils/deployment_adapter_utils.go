// Package utils hosts shared helpers used by both the local and kubernetes
// platform adapters. The primary surface is TranslateMCPServer, which takes
// a v1alpha1.MCPServerSpec plus runtime overrides and projects it onto the
// platform-internal *platformtypes.MCPServer that adapters then dispatch.
//
// Historically this translator operated on the upstream
// modelcontextprotocol/registry apiv0.ServerJSON shape, with a projection
// layer that converted v1alpha1 → ServerJSON. That projection was removed
// when the refactor landed Scott's directive "everything should be v1alpha1";
// the translator now speaks v1alpha1 types directly.
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
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// DefaultLocalAgentPort is the port kagent-runtime listens on inside the
// agent container. Kept as a package const so both adapters + tests
// reference the same value.
const DefaultLocalAgentPort uint16 = 8080

// MCPServerRunRequest is the input bundle TranslateMCPServer needs. Spec
// carries the authoritative description of what's being run; the *Values
// maps carry per-deployment runtime overrides supplied on apply.
//
// MCPServer (the bundled kind) carries Packages; the translator now only
// produces local transport. RemoteMCPServer is its own kind handled by
// TranslateRemoteMCPServer.
type MCPServerRunRequest struct {
	// Name is metadata.name of the v1alpha1.MCPServer; used to derive the
	// platform-internal container/resource name via generateInternalName.
	Name string
	// Spec is the v1alpha1 MCPServerSpec authored in the manifest.
	Spec v1alpha1.MCPServerSpec
	// DeploymentID is the unique identifier this invocation carries; the
	// same Spec deployed twice produces two distinct DeploymentIDs and
	// therefore two distinct platform-internal names.
	DeploymentID string
	// EnvValues, ArgValues carry per-deployment runtime overrides for the
	// bundled local server. Nil is treated as an empty map.
	EnvValues map[string]string
	ArgValues map[string]string
	// HeaderValues is retained on the request struct for callers that
	// historically passed it through; it is unused by local translation
	// and ignored. Header overrides for remote endpoints flow through
	// TranslateRemoteMCPServer instead.
	HeaderValues map[string]string
}

// TranslateMCPServer maps a v1alpha1 MCPServerSpec onto the platform-internal
// MCPServer. The kind only carries packages — output is always
// MCPServerType=local.
func TranslateMCPServer(ctx context.Context, req *MCPServerRunRequest) (*platformtypes.MCPServer, error) {
	if req == nil {
		return nil, fmt.Errorf("mcp server run request is required")
	}
	if len(req.Spec.Packages) == 0 {
		return nil, fmt.Errorf("no valid deployment method found for server: %s (no packages)", req.Name)
	}
	return translateLocalMCPServer(ctx, req.Name, req.Spec, req.DeploymentID, req.EnvValues, req.ArgValues)
}

// RemoteMCPServerRunRequest carries inputs for projecting a v1alpha1
// RemoteMCPServer onto a platform-internal MCPServer with remote transport.
type RemoteMCPServerRunRequest struct {
	// Name is metadata.name of the v1alpha1.RemoteMCPServer; used to derive
	// the platform-internal resource name via generateInternalName.
	Name string
	// Spec is the v1alpha1 RemoteMCPServerSpec authored in the manifest.
	Spec v1alpha1.RemoteMCPServerSpec
	// DeploymentID is the unique identifier this invocation carries.
	DeploymentID string
	// HeaderValues are per-deployment header overrides resolved against
	// the remote's declared header inputs.
	HeaderValues map[string]string
}

// TranslateRemoteMCPServer projects a v1alpha1 RemoteMCPServer onto a
// platform-internal MCPServer configured for remote transport. Header
// overrides resolve against the remote's header input list with
// required/default semantics matching the MCP spec.
func TranslateRemoteMCPServer(_ context.Context, req *RemoteMCPServerRunRequest) (*platformtypes.MCPServer, error) {
	if req == nil {
		return nil, fmt.Errorf("remote mcp server run request is required")
	}
	if req.Spec.Remote.URL == "" {
		return nil, fmt.Errorf("remote mcp server %s has no URL", req.Name)
	}

	headersMap, err := processHeaders(req.Spec.Remote.Headers, req.HeaderValues)
	if err != nil {
		return nil, err
	}
	headers := make([]platformtypes.HeaderValue, 0, len(headersMap))
	for k, v := range headersMap {
		headers = append(headers, platformtypes.HeaderValue{Name: k, Value: v})
	}

	u, err := parseURL(req.Spec.Remote.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote server url: %v", err)
	}

	return &platformtypes.MCPServer{
		Name:          generateInternalName(req.Name),
		DeploymentID:  req.DeploymentID,
		MCPServerType: platformtypes.MCPServerTypeRemote,
		Remote: &platformtypes.RemoteMCPTarget{
			Scheme:  u.scheme,
			Host:    u.host,
			Port:    u.port,
			Path:    u.path,
			Headers: headers,
		},
	}, nil
}

// translateLocalMCPServer emits a platformtypes.MCPServer for package-based
// local execution. Registry-type dispatch (npm / pypi / oci) picks the base
// image and command; runtime + package arguments merge with overrides;
// environment variables resolve required/default values. The transport
// field inside the package controls whether the runner speaks stdio or
// http on the far side.
func translateLocalMCPServer(
	_ context.Context,
	serverName string,
	spec v1alpha1.MCPServerSpec,
	deploymentID string,
	envValues map[string]string,
	argValues map[string]string,
) (*platformtypes.MCPServer, error) {
	pkg := spec.Packages[0]

	var args []string
	processedArgs := make(map[string]bool)
	addProcessedArgs := func(in []v1alpha1.MCPArgument) {
		for _, arg := range in {
			processedArgs[arg.Name] = true
		}
	}

	args = processArguments(args, pkg.RuntimeArguments, argValues)
	addProcessedArgs(pkg.RuntimeArguments)

	config, args, err := GetRegistryConfig(pkg, args)
	if err != nil {
		return nil, err
	}

	args = processArguments(args, pkg.PackageArguments, argValues)
	addProcessedArgs(pkg.PackageArguments)

	// Any override the spec doesn't declare gets appended at the end as a
	// raw (name, value) pair so callers can inject one-off flags without
	// editing the manifest. Ordered deterministically.
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

	processedEnvVars, err := processEnvironmentVariables(pkg.EnvironmentVariables, envValues)
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
	switch pkg.Transport.Type {
	case "stdio":
		transportType = platformtypes.TransportTypeStdio
	default:
		transportType = platformtypes.TransportTypeHTTP
		u, err := parseURL(pkg.Transport.URL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse transport url: %v", err)
		}
		httpTransport = &platformtypes.HTTPTransport{
			Port: u.port,
			Path: u.path,
		}
	}

	return &platformtypes.MCPServer{
		Name:          generateInternalName(serverName),
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

// parsedURL is the narrow shape TranslateMCPServer needs from a transport URL.
type parsedURL struct {
	scheme string
	host   string
	port   uint32
	path   string
}

// parseURL enforces http/https-only and normalizes missing ports to the
// protocol default.
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

// BuildRemoteMCPURL constructs a well-formed URL from a RemoteMCPTarget,
// handling IPv6 bracketing and standard-port omission.
func BuildRemoteMCPURL(remote *platformtypes.RemoteMCPTarget) string {
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

// generateInternalName normalizes an MCPServer or Agent name into a
// platform-safe slug: lowercase, replace any of ~20 common punctuation
// characters with '-'. Output is safe for Docker, Kubernetes DNS-1123,
// and agentgateway labels.
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

// GenerateInternalNameForDeployment stamps the deploymentID suffix onto the
// base internal name so two deployments of the same MCPServer don't collide
// at the platform level.
func GenerateInternalNameForDeployment(name, deploymentID string) string {
	base := generateInternalName(name)
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, generateInternalName(deploymentID))
}

// RegistryConfig captures what runtime image + command a package dispatches
// to. IsOCI toggles container-passthrough (Command is a hint for the runner,
// Image IS the server).
type RegistryConfig struct {
	Image   string
	Command string
	IsOCI   bool
}

// processArguments appends a package's argument list onto the running args
// slice, resolving overrides by name. Positional args come first, then named
// args; the caller later appends any extras the override map carries that
// the spec didn't declare.
func processArguments(
	args []string,
	modelArgs []v1alpha1.MCPArgument,
	argOverrides map[string]string,
) []string {
	getArgValue := func(arg v1alpha1.MCPArgument) string {
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
		if arg.Type == v1alpha1.MCPArgumentTypePositional {
			value := getArgValue(arg)
			if value != "" {
				args = append(args, value)
			}
		}
	}
	for _, arg := range modelArgs {
		if arg.Type == v1alpha1.MCPArgumentTypeNamed {
			args = append(args, arg.Name)
			value := getArgValue(arg)
			if value != "" {
				args = append(args, value)
			}
		}
	}
	return args
}

// processEnvironmentVariables resolves the package's env-var list against
// supplied overrides. Required env vars with no value anywhere (override,
// spec value, spec default) surface as an aggregate error listing all
// missing names. Overrides for env vars the spec didn't declare pass
// through as-is.
func processEnvironmentVariables(
	envVars []v1alpha1.MCPKeyValueInput,
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

// processHeaders resolves a remote's header input list. Same required/default
// semantics as processEnvironmentVariables.
func processHeaders(
	headers []v1alpha1.MCPKeyValueInput,
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

// GetRegistryConfig picks the base image + command for a package based on
// its registry type:
//   - npm  → node:24-alpine3.21 + `npx -y <id>[@ver]`
//   - pypi → ghcr.io/astral-sh/uv:debian + `uvx <id>[==ver]`
//   - oci  → the image is the package identifier itself; the runtime hint
//     becomes the command if set
//
// RuntimeHint on the package overrides the default command if specified.
// Unsupported registry types return an error.
func GetRegistryConfig(
	pkg v1alpha1.MCPPackage,
	args []string,
) (RegistryConfig, []string, error) {
	var config RegistryConfig
	normalizedType := strings.ToLower(strings.TrimSpace(pkg.RegistryType))

	switch normalizedType {
	case v1alpha1.RegistryTypeNPM:
		config.Image = "node:24-alpine3.21"
		config.Command = pkg.RuntimeHint
		if config.Command == "" {
			config.Command = "npx"
		}
		if !slices.Contains(args, "-y") {
			args = append(args, "-y")
		}
		if pkg.Version != "" {
			args = append(args, pkg.Identifier+"@"+pkg.Version)
		} else {
			args = append(args, pkg.Identifier)
		}
	case v1alpha1.RegistryTypePyPI:
		config.Image = "ghcr.io/astral-sh/uv:debian"
		config.Command = pkg.RuntimeHint
		if config.Command == "" {
			config.Command = "uvx"
		}
		if pkg.Version != "" {
			args = append(args, pkg.Identifier+"=="+pkg.Version)
		} else {
			args = append(args, pkg.Identifier)
		}
	case v1alpha1.RegistryTypeOCI:
		config.Image = pkg.Identifier
		config.Command = pkg.RuntimeHint
		config.IsOCI = true
	default:
		return RegistryConfig{}, nil, fmt.Errorf("unsupported package registry type: %s", pkg.RegistryType)
	}

	return config, args, nil
}

// EnvMapToStringSlice renders an env map as a sorted ["K=V"] slice —
// suitable for docker and kubernetes env surfaces.
func EnvMapToStringSlice(envMap map[string]string) []string {
	result := make([]string, 0, len(envMap))
	for key, value := range envMap {
		result = append(result, fmt.Sprintf("%s=%s", key, value))
	}
	slices.Sort(result)
	return result
}
