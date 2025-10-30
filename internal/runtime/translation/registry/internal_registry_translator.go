package registry

import (
	"context"
	"fmt"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"net/url"
	"slices"
	"strconv"
	"strings"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type MCPServerRunRequest struct {
	RegistryServer *apiv0.ServerJSON
	PreferRemote   bool
	EnvValues      map[string]string
	ArgValues      map[string]string
	HeaderValues   map[string]string
}

// Translator is the interface for translating MCPServer objects to AgentGateway objects.
type Translator interface {
	TranslateMCPServer(
		ctx context.Context,
		req *MCPServerRunRequest,
	) (*api.MCPServer, error)
}

type registryTranslator struct{}

func NewTranslator() Translator {
	return &registryTranslator{}
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

	var headers []api.HeaderValue
	for _, h := range remoteInfo.Headers {
		k := h.Name
		v := h.Value
		if v == "" {
			v = h.Default
		}
		if headerValues != nil {
			if override, exists := headerValues[k]; exists {
				v = override
			}
		}
		if h.IsRequired && v == "" {
			return nil, fmt.Errorf("missing required header value for header: %s", k)
		}
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
	var (
		image string
		cmd   string
		args  []string
	)

	// deploy the server either as stdio or http
	packageInfo := registryServer.Packages[0]

	cmd = packageInfo.RunTimeHint

	getArgValue := func(arg model.Argument) string {
		if v, exists := argValues[arg.Name]; exists {
			return v
		}
		return arg.Value
	}
	addArgs := func(modelArgrs []model.Argument) {
		for _, arg := range modelArgrs {
			switch arg.Type {
			case model.ArgumentTypePositional:
				args = append(args, getArgValue(arg))
			}
		}
		for _, arg := range modelArgrs {
			switch arg.Type {
			case model.ArgumentTypeNamed:
				args = append(args, arg.Name, getArgValue(arg))
			}
		}
	}

	addArgs(packageInfo.RuntimeArguments)

	switch packageInfo.RegistryType {
	case "npm":
		image = "node:24-alpine3.21"
		if cmd == "" {
			cmd = "npx"
		}
		if !slices.Contains(args, "-y") {
			args = append(args, "-y")
		}
		args = append(args, packageInfo.Identifier)
	case "pypi":
		image = "ghcr.io/astral-sh/uv:debian"
		if cmd == "" {
			cmd = "uvx"
		}
		args = []string{packageInfo.Identifier}
	case "oci":
		image = packageInfo.Identifier
	default:
		return nil, fmt.Errorf("unsupported package registry type: %s", packageInfo.RegistryType)
	}

	addArgs(packageInfo.PackageArguments)

	for _, envVar := range packageInfo.EnvironmentVariables {
		if _, exists := envValues[envVar.Name]; !exists {
			if envVar.IsRequired {
				return nil, fmt.Errorf("missing required environment variable: %s", envVar.Name)
			} else if envVar.Default != "" {
				envValues[envVar.Name] = envVar.Default
			}
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
				Image: image,
				Cmd:   cmd,
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
