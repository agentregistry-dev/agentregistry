package utils

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/registry/types"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
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

	useRemote := len(serverSpec.Remotes) > 0 && (preferRemote || len(serverSpec.Packages) == 0)
	if useRemote { //nolint:nestif
		remote := serverSpec.Remotes[0]
		if remote.URL == "" {
			return nil, fmt.Errorf("server %q remote has no URL", serverSpec.Name)
		}

		headers, err := utils.ProcessHeaders(remote.Headers, nil)
		if err != nil {
			return nil, err
		}

		return &models.McpServerType{
			Type:    "remote",
			Name:    name,
			URL:     remote.URL,
			Headers: headers,
		}, nil
	} else {
		pkg := serverSpec.Packages[0]

		var args []string

		// Process runtime arguments first
		args = utils.ProcessArguments(args, pkg.RuntimeArguments, nil)

		// Determine image and command based on registry type
		config, args, err := utils.GetRegistryConfig(pkg, args)
		if err != nil {
			return nil, err
		}

		// Process package arguments after the package identifier
		args = utils.ProcessArguments(args, pkg.PackageArguments, nil)

		// Process environment variables
		envVarsMap, err := utils.ProcessEnvironmentVariables(pkg.EnvironmentVariables, envOverrides)
		if err != nil {
			return nil, err
		}
		envVars := utils.EnvMapToStringSlice(envVarsMap)

		// For OCI registry type, we already have a complete image - no build needed.
		// For other types (npm, pypi), we need to create a build context with Dockerfile.
		buildPath := ""
		if !config.IsOCI {
			buildPath = "registry/" + name // Registry-resolved servers go under registry/ to easily manage on sequential runs
		}

		return &models.McpServerType{
			Type:    "command",
			Name:    name,
			Image:   config.Image,
			Build:   buildPath,
			Command: config.Command,
			Args:    args,
			Env:     envVars,
		}, nil
	}
}
