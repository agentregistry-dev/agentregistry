// Package utils hosts shared helpers used by both the local and
// kubernetes runtime adapters. The primary surface is
// TranslateMCPServer, which takes a v1alpha1.MCPServerSpec plus
// per-deployment runtime overrides and projects it onto the
// runtime-internal *runtimetypes.MCPServer that adapters dispatch.
package utils

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"

	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// DefaultLocalAgentPort is the port kagent-runtime listens on inside the
// agent container. Kept as a package const so both adapters + tests
// reference the same value.
const DefaultLocalAgentPort uint16 = 8080

// MCPServerRunRequest is the input bundle TranslateMCPServer needs. Spec
// carries the authoritative description of what's being run; the *Values
// maps carry per-deployment runtime overrides supplied on apply.
//
// MCPServer is the single kind for both bundled (Spec.Source.Package) and
// remote (Spec.Remote) servers. TranslateMCPServer dispatches on which
// field is populated and produces MCPServerType=local or =remote accordingly.
type MCPServerRunRequest struct {
	// Name is metadata.name of the v1alpha1.MCPServer; used to derive the
	// runtime-internal container/resource name via generateInternalName.
	Name string
	// Spec is the v1alpha1 MCPServerSpec authored in the manifest.
	Spec v1alpha1.MCPServerSpec
	// DeploymentID is the unique identifier this invocation carries; the
	// same Spec deployed twice produces two distinct DeploymentIDs and
	// therefore two distinct runtime-internal names.
	DeploymentID string
	// EnvValues, ArgValues carry per-deployment runtime overrides for the
	// bundled local server. Nil is treated as an empty map.
	EnvValues map[string]string
	ArgValues map[string]string
	// HeaderValues are per-deployment header overrides resolved against
	// Spec.Remote.Headers when the server is remote. Ignored for bundled.
	HeaderValues map[string]string
}

// TranslateMCPServer maps a v1alpha1 MCPServerSpec onto the runtime-internal
// MCPServer. Dispatches on Spec.Source (bundled → local transport) vs
// Spec.Remote (pre-running → remote transport). Validation enforces exactly
// one of the two is set.
func TranslateMCPServer(ctx context.Context, req *MCPServerRunRequest) (*runtimetypes.MCPServer, error) {
	if req == nil {
		return nil, fmt.Errorf("mcp server run request is required")
	}
	if req.Spec.Remote != nil {
		return translateRemoteMCPServer(req.Name, req.Spec.Remote, req.DeploymentID, req.HeaderValues)
	}
	if req.Spec.Source == nil || req.Spec.Source.Package == nil {
		return nil, fmt.Errorf("no valid deployment method found for server: %s (no package or remote)", req.Name)
	}
	return translateLocalMCPServer(ctx, req.Name, req.Spec, req.DeploymentID, req.EnvValues, req.ArgValues)
}

// translateRemoteMCPServer emits a runtimetypes.MCPServer for a
// pre-running remote endpoint. Header overrides resolve against the
// remote's declared headers, with overrides taking precedence over
// spec values.
func translateRemoteMCPServer(name string, remote *v1alpha1.MCPRemote, deploymentID string, headerValues map[string]string) (*runtimetypes.MCPServer, error) {
	if remote.URL == "" {
		return nil, fmt.Errorf("remote mcp server %s has no URL", name)
	}

	headersMap := processHeaders(remote.Headers, headerValues)
	headers := make([]runtimetypes.HeaderValue, 0, len(headersMap))
	for k, v := range headersMap {
		headers = append(headers, runtimetypes.HeaderValue{Name: k, Value: v})
	}

	u, err := parseURL(remote.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse remote server url: %v", err)
	}

	return &runtimetypes.MCPServer{
		Name:          generateInternalName(name),
		DeploymentID:  deploymentID,
		MCPServerType: runtimetypes.MCPServerTypeRemote,
		Remote: &runtimetypes.RemoteMCPTarget{
			Scheme:  u.scheme,
			Host:    u.host,
			Port:    u.port,
			Path:    u.path,
			Headers: headers,
		},
	}, nil
}

// translateLocalMCPServer emits a runtimetypes.MCPServer for
// package-based local execution. Origin selects the runner image; if
// Launch is set, the manifest owns Cmd/Args verbatim (with override
// merging by arg name); if Launch is nil, the resolver derives
// per-type defaults (npm: "npx -y <id>@<ver>", pypi: "uvx <id>==<ver>",
// oci: image entrypoint). The transport field controls whether the
// runner speaks stdio or http on the far side.
func translateLocalMCPServer(
	_ context.Context,
	serverName string,
	spec v1alpha1.MCPServerSpec,
	deploymentID string,
	envValues map[string]string,
	argValues map[string]string,
) (*runtimetypes.MCPServer, error) {
	pkg := *spec.Source.Package

	if envValues == nil {
		envValues = make(map[string]string)
	}

	config, defaultArgs, err := GetRegistryConfig(pkg.Origin)
	if err != nil {
		return nil, err
	}

	var (
		cmd  string
		args []string
	)

	if pkg.Launch == nil {
		// Resolver-defaults path: derive Command + Args from Origin.Type.
		cmd = config.Command
		args = defaultArgs
	} else {
		var err error
		cmd, args, err = applyManifestLaunch(pkg.Launch, argValues, envValues)
		if err != nil {
			return nil, err
		}
	}

	var (
		transportType runtimetypes.TransportType
		httpTransport *runtimetypes.HTTPTransport
	)
	switch pkg.Transport.Type {
	case "stdio":
		transportType = runtimetypes.TransportTypeStdio
	default:
		transportType = runtimetypes.TransportTypeHTTP
		httpTransport = &runtimetypes.HTTPTransport{
			Port: uint32(pkg.Transport.Port),
			Path: pkg.Transport.Path,
		}
	}

	return &runtimetypes.MCPServer{
		Name:          generateInternalName(serverName),
		DeploymentID:  deploymentID,
		MCPServerType: runtimetypes.MCPServerTypeLocal,
		Local: &runtimetypes.LocalMCPServer{
			Deployment: runtimetypes.MCPServerDeployment{
				Image: config.Image,
				Cmd:   cmd,
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
// applyManifestLaunch implements the manifest-owned Cmd/Args/Env path for
// translateLocalMCPServer. Empty Command is allowed only for OCI (image
// ENTRYPOINT runs); the resolver doesn't enforce this — the validator's
// job — but we leave cmd empty rather than falling back to the default to
// preserve manifest intent. Mutates envValues in place: spec-declared
// values back-populate keys the caller didn't override.
func applyManifestLaunch(
	launch *v1alpha1.MCPPackageLaunch,
	argValues map[string]string,
	envValues map[string]string,
) (string, []string, error) {
	cmd := launch.Command
	args := processArguments(nil, launch.Args, argValues)

	processedArgNames := make(map[string]bool, len(launch.Args))
	for _, a := range launch.Args {
		processedArgNames[a.Name] = true
	}
	var extraArgNames []string
	for argName := range argValues {
		if !processedArgNames[argName] {
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

	processedEnvVars, err := processEnvironmentVariables(launch.Env, envValues)
	if err != nil {
		return "", nil, err
	}
	for key, value := range processedEnvVars {
		if _, exists := envValues[key]; !exists {
			envValues[key] = value
		}
	}
	return cmd, args, nil
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

// BuildRemoteMCPURL constructs a well-formed URL from a RemoteMCPTarget,
// handling IPv6 bracketing and standard-port omission.
func BuildRemoteMCPURL(remote *runtimetypes.RemoteMCPTarget) string {
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
// runtime-safe slug: lowercase, replace any of ~20 common punctuation
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
	name = strings.ReplaceAll(name, ",", "-")
	name = strings.ReplaceAll(name, "!", "-")
	name = strings.ReplaceAll(name, "?", "-")
	return name
}

// GenerateInternalNameForDeployment stamps the deploymentID suffix onto the
// base internal name so two deployments of the same MCPServer don't collide
// at the runtime level.
func GenerateInternalNameForDeployment(name, deploymentID string) string {
	base := generateInternalName(name)
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return base
	}
	return fmt.Sprintf("%s-%s", base, generateInternalName(deploymentID))
}

// RegistryConfig captures what runtime image + default launch command a
// package's Origin dispatches to. IsOCI toggles container-passthrough
// (Command is a hint for the runner, Image IS the server).
type RegistryConfig struct {
	Image   string
	Command string
	IsOCI   bool
}

// processArguments appends a package's argument list onto the running
// args slice, resolving overrides by name. Positional args come first,
// then named args.
//
// Per-arg value resolution: argOverrides[arg.Name] → arg.Value → "".
// (The historical arg.Default fallback was dropped when MCPArgument
// was trimmed to {Type, Name, Value}.)
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
		return arg.Value
	}

	for _, arg := range modelArgs {
		if arg.Type == v1alpha1.MCPArgumentTypePositional {
			if value := getArgValue(arg); value != "" {
				args = append(args, value)
			}
		}
	}
	for _, arg := range modelArgs {
		if arg.Type == v1alpha1.MCPArgumentTypeNamed {
			args = append(args, arg.Name)
			if value := getArgValue(arg); value != "" {
				args = append(args, value)
			}
		}
	}
	return args
}

// processEnvironmentVariables resolves the package's env-var list
// against supplied overrides. Required env vars with no value
// anywhere (override, spec value) surface as an aggregate error
// listing all missing names. Overrides for env vars the spec didn't
// declare pass through as-is.
//
// Per-key resolution: overrides[env.Name] → env.Value → "".
// (The historical env.Default fallback was dropped when
// MCPKeyValueInput was trimmed to {Name, Value, IsRequired}.)
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

// processHeaders resolves a remote's declared headers against the supplied
// overrides. Override values take precedence over spec values. Headers with
// an empty effective value are dropped.
func processHeaders(
	headers []v1alpha1.HTTPHeader,
	headerOverrides map[string]string,
) map[string]string {
	result := make(map[string]string)
	for _, h := range headers {
		value := h.Value
		if override, exists := headerOverrides[h.Name]; exists {
			value = override
		}
		if value != "" {
			result[h.Name] = value
		}
	}
	return result
}

// GetRegistryConfig returns the runner image and the resolver's
// derived-default Command + Args for an Origin. These defaults apply
// when MCPPackage.Launch is nil — Launch sets the wire-level
// {Command, Args} and bypasses these defaults entirely. Unsupported
// Origin (no sub-struct set) returns an error.
func GetRegistryConfig(origin v1alpha1.MCPPackageOrigin) (RegistryConfig, []string, error) {
	switch {
	case origin.NPM != nil:
		ref := origin.Identifier
		if origin.NPM.Version != "" {
			ref = ref + "@" + origin.NPM.Version
		}
		return RegistryConfig{
			Image:   types.DefaultNPMRunnerImage,
			Command: "npx",
		}, []string{"-y", ref}, nil
	case origin.PyPI != nil:
		ref := origin.Identifier
		if origin.PyPI.Version != "" {
			ref = ref + "==" + origin.PyPI.Version
		}
		return RegistryConfig{
			Image:   types.DefaultPyPIRunnerImage,
			Command: "uvx",
		}, []string{ref}, nil
	case origin.OCI != nil:
		return RegistryConfig{
			Image: origin.Identifier,
			IsOCI: true,
		}, nil, nil
	default:
		return RegistryConfig{}, nil, fmt.Errorf("unsupported MCPPackage origin: no sub-struct (NPM/PyPI/OCI) is set; Origin.Type=%q", origin.Type)
	}
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
