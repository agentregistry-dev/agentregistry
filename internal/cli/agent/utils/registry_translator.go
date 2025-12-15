package utils

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/registry/types"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry/utils"
)

// TranslateRegistryServer converts a registry ServerSpec into a common.McpServerType
// that can be used by the docker-compose generator.
func TranslateRegistryServer(
	name string,
	serverSpec *types.ServerSpec,
	envOverrides map[string]string,
	preferRemote bool,
) (*common.McpServerType, error) {
	if len(serverSpec.Remotes) == 0 && len(serverSpec.Packages) == 0 {
		return nil, fmt.Errorf("server %q has no remotes or packages", serverSpec.Name)
	}

	useRemote := len(serverSpec.Remotes) > 0 && (preferRemote || len(serverSpec.Packages) == 0)
	if useRemote {
		remote := serverSpec.Remotes[0]
		if remote.URL == "" {
			return nil, fmt.Errorf("server %q remote has no URL", serverSpec.Name)
		}

		headers, err := utils.ProcessHeaders(remote.Headers, nil)
		if err != nil {
			return nil, err
		}

		return &common.McpServerType{
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

		return &common.McpServerType{
			Type:    "command",
			Name:    name,
			Image:   config.Image,
			Build:   "registry/" + name, // Registry-resolved servers go under registry/ to easily manage on sequential runs
			Command: config.Command,
			Args:    args,
			Env:     envVars,
		}, nil
	}
}
