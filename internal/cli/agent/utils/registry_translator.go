package utils

import (
	"context"
	"fmt"
	"maps"

	platformutils "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/internal/registry/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// TranslateRegistryServer converts a registry ServerSpec into a common.McpServerType
// that can be used by the docker-compose generator.
func TranslateRegistryServer(
	name string,
	serverSpec *types.ServerSpec,
	envOverrides map[string]string,
	preferRemote bool,
) (*models.McpServerType, error) {
	if len(serverSpec.Remotes) == 0 && len(serverSpec.Packages) == 0 {
		return nil, fmt.Errorf("server %q has no remotes or packages", serverSpec.Name)
	}

	runEnv := make(map[string]string, len(envOverrides))
	maps.Copy(runEnv, envOverrides)

	translated, err := platformutils.TranslateMCPServer(context.Background(), &platformutils.MCPServerRunRequest{
		RegistryServer: &apiv0.ServerJSON{
			Name:        serverSpec.Name,
			Title:       serverSpec.Title,
			Description: serverSpec.Description,
			Version:     serverSpec.Version,
			Packages:    serverSpec.Packages,
			Remotes:     serverSpec.Remotes,
		},
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
		if len(serverSpec.Remotes) == 0 || serverSpec.Remotes[0].URL == "" {
			return nil, fmt.Errorf("server %q remote has no URL", serverSpec.Name)
		}
		headers := make(map[string]string, len(translated.Remote.Headers))
		for _, header := range translated.Remote.Headers {
			headers[header.Name] = header.Value
		}
		return &models.McpServerType{
			Type:    "remote",
			Name:    name,
			URL:     serverSpec.Remotes[0].URL,
			Headers: headers,
		}, nil
	case "local":
		if translated.Local == nil {
			return nil, fmt.Errorf("server %q local translation missing deployment config", serverSpec.Name)
		}
		buildPath := ""
		if len(serverSpec.Packages) > 0 {
			config, _, err := platformutils.GetRegistryConfig(serverSpec.Packages[0], nil)
			if err != nil {
				return nil, err
			}
			if !config.IsOCI {
				buildPath = "registry/" + name
			}
		}
		return &models.McpServerType{
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
