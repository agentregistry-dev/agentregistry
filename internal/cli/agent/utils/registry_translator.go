package utils

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/registry/types"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry/utils"
)

// TranslateRegistryServer converts a registry ServerSpec into a common.McpServerType
// that can be used by the docker-compose generator.
//
// envOverrides allows passing environment variables that override defaults or satisfy required vars.
// Returns an error if required environment variables are missing.
func TranslateRegistryServer(
	serverSpec *types.ServerSpec,
	name string,
	envOverrides map[string]string,
) (*common.McpServerType, error) {
	// if there are remotes, use the first one
	if len(serverSpec.Remotes) > 0 {
		remote := serverSpec.Remotes[0]
		if remote.URL == "" {
			return nil, fmt.Errorf("server %q remote has no URL", serverSpec.Name)
		}

		headers, err := utils.ProcessHeaders(remote.Headers, nil, serverSpec.Name)
		if err != nil {
			return nil, err
		}

		return &common.McpServerType{
			Type:    "remote",
			Name:    name,
			URL:     remote.URL,
			Headers: headers,
		}, nil
	}

	// if there are packages (command-based servers), use the first one
	if len(serverSpec.Packages) > 0 {
		pkg := serverSpec.Packages[0]

		var args []string

		// Process runtime arguments first
		args = utils.ProcessArguments(args, pkg.RuntimeArguments, nil)

		// Determine image and command based on registry type
		config, args, err := utils.GetRegistryConfig(string(pkg.RegistryType), pkg.RunTimeHint, pkg.Identifier, args)
		if err != nil {
			return nil, err
		}

		// Process package arguments after the package identifier
		args = utils.ProcessArguments(args, pkg.PackageArguments, nil)

		// Process environment variables
		envVarsMap, err := utils.ProcessEnvironmentVariables(pkg.EnvironmentVariables, envOverrides, serverSpec.Name)
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

	return nil, fmt.Errorf("server %q has no packages or remotes defined", serverSpec.Name)
}
