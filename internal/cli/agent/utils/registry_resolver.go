package utils

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/registry"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// ParseAgentManifestServers resolves registry-type MCP servers in an agent manifest, keeping non-registry servers as-is.
func ParseAgentManifestServers(manifest *common.AgentManifest, verbose bool) ([]common.McpServerType, error) {
	servers := []common.McpServerType{}

	for _, mcpServer := range manifest.McpServers {
		switch mcpServer.Type {
		case "registry":
			// Fetch server spec from registry and translate to command/remote type
			translatedServer, err := resolveRegistryServer(mcpServer, verbose)
			if err != nil {
				return nil, fmt.Errorf("failed to resolve registry server %q: %w", mcpServer.Name, err)
			}
			servers = append(servers, *translatedServer)
		default:
			servers = append(servers, mcpServer)
		}
	}

	return servers, nil
}

// resolveRegistryServer fetches a server from the registry and translates it to a runnable config
func resolveRegistryServer(mcpServer common.McpServerType, verbose bool) (*common.McpServerType, error) {
	registryURL := mcpServer.RegistryURL
	if registryURL == "" {
		registryURL = "http://127.0.0.1:12121"
	}

	client := registry.NewClient()
	serverEntry, err := client.FetchServer(registryURL, mcpServer.RegistryServerName, mcpServer.RegistryServerVersion)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server %q from registry: %w", mcpServer.RegistryServerName, err)
	}

	// Collect environment variable overrides from the current environment
	// This allows users to set required env vars before running
	envOverrides := collectEnvOverrides(serverEntry.Server.Packages)

	// Translate the registry server spec to a runnable McpServerType
	translated, err := TranslateRegistryServer(mcpServer.Name, &serverEntry.Server, envOverrides, mcpServer.RegistryServerPreferRemote)
	if err != nil {
		return nil, err
	}

	if verbose {
		fmt.Printf("Resolved registry server %q (%s) -> %s (image: %s, command: %s)\n",
			mcpServer.RegistryServerName, serverEntry.Server.Version, translated.Type, translated.Image, translated.Command)
	}

	return translated, nil
}

// collectEnvOverrides gathers environment variable values from the current environment
// for any env vars defined in the package specs
func collectEnvOverrides(packages []model.Package) map[string]string {
	overrides := make(map[string]string)

	for _, pkg := range packages {
		for _, envVar := range pkg.EnvironmentVariables {
			if value := os.Getenv(envVar.Name); value != "" {
				overrides[envVar.Name] = value
			}
		}
	}

	return overrides
}
