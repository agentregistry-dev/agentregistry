package utils

import (
	"context"
	"fmt"
	"maps"

	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
	platformutils "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// TranslateRegistryServer converts a v1alpha1.MCPServer envelope into the
// runnable agentmanifest.McpServerType the docker-compose generator consumes.
func TranslateRegistryServer(
	name string,
	server *v1alpha1.MCPServer,
	envOverrides map[string]string,
	preferRemote bool,
) (*agentmanifest.McpServerType, error) {
	if server == nil {
		return nil, fmt.Errorf("server %q: nil envelope", name)
	}
	spec := server.Spec
	if len(spec.Remotes) == 0 && len(spec.Packages) == 0 {
		return nil, fmt.Errorf("server %q has no remotes or packages", server.Metadata.Name)
	}

	runEnv := make(map[string]string, len(envOverrides))
	maps.Copy(runEnv, envOverrides)

	translated, err := platformutils.TranslateMCPServer(context.Background(), &platformutils.MCPServerRunRequest{
		Name:         server.Metadata.Name,
		Spec:         spec,
		PreferRemote: preferRemote,
		EnvValues:    runEnv,
		ArgValues:    map[string]string{},
		HeaderValues: map[string]string{},
	})
	if err != nil {
		return nil, err
	}

	switch translated.MCPServerType {
	case "remote":
		if len(spec.Remotes) == 0 || spec.Remotes[0].URL == "" {
			return nil, fmt.Errorf("server %q remote has no URL", server.Metadata.Name)
		}
		headers := make(map[string]string, len(translated.Remote.Headers))
		for _, header := range translated.Remote.Headers {
			headers[header.Name] = header.Value
		}
		return &agentmanifest.McpServerType{
			Type:    "remote",
			Name:    name,
			URL:     spec.Remotes[0].URL,
			Headers: headers,
		}, nil
	case "local":
		if translated.Local == nil {
			return nil, fmt.Errorf("server %q local translation missing deployment config", server.Metadata.Name)
		}
		buildPath := ""
		if len(spec.Packages) > 0 {
			config, _, err := platformutils.GetRegistryConfig(spec.Packages[0], nil)
			if err != nil {
				return nil, err
			}
			if !config.IsOCI {
				buildPath = "registry/" + name
			}
		}
		return &agentmanifest.McpServerType{
			Type:    "command",
			Name:    name,
			Image:   translated.Local.Deployment.Image,
			Build:   buildPath,
			Command: translated.Local.Deployment.Cmd,
			Args:    translated.Local.Deployment.Args,
			Env:     platformutils.EnvMapToStringSlice(translated.Local.Deployment.Env),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported translated server type %q", translated.MCPServerType)
	}
}
