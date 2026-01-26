package utils

import (
	"fmt"
	"maps"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/registry"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

var defaultRegistryURL = "http://127.0.0.1:12121"

// SetDefaultRegistryURL overrides the fallback registry URL used when manifests omit registry_url.
func SetDefaultRegistryURL(url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	defaultRegistryURL = url
}

// ParseAgentManifestServers resolves registry-type MCP servers in an agent manifest, keeping non-registry servers as-is.
func ParseAgentManifestServers(manifest *common.AgentManifest, verbose bool) ([]common.McpServerType, error) {
	servers := []common.McpServerType{}

	if verbose {
		fmt.Printf("[registry-resolver] Processing %d MCP servers from manifest\n", len(manifest.McpServers))
	}

	for i, mcpServer := range manifest.McpServers {
		switch mcpServer.Type {
		case "registry":
			if verbose {
				fmt.Printf("[registry-resolver] [%d] Resolving registry server: name=%q registryServerName=%q version=%q registryURL=%q preferRemote=%v\n",
					i, mcpServer.Name, mcpServer.RegistryServerName, mcpServer.RegistryServerVersion, mcpServer.RegistryURL, mcpServer.RegistryServerPreferRemote)
			}
			// Fetch server spec from registry and translate to command/remote type
			translatedServer, err := resolveRegistryServer(mcpServer, verbose)
			if err != nil {
				if verbose {
					fmt.Printf("[registry-resolver] [%d] FAILED to resolve registry server %q: %v\n", i, mcpServer.Name, err)
				}
				return nil, fmt.Errorf("failed to resolve registry server %q: %w", mcpServer.Name, err)
			}
			if verbose {
				fmt.Printf("[registry-resolver] [%d] Successfully resolved %q -> type=%q build=%q image=%q command=%q args=%v\n",
					i, mcpServer.Name, translatedServer.Type, translatedServer.Build, translatedServer.Image, translatedServer.Command, translatedServer.Args)
			}
			servers = append(servers, *translatedServer)
		default:
			if verbose {
				fmt.Printf("[registry-resolver] [%d] Keeping non-registry server as-is: name=%q type=%q\n", i, mcpServer.Name, mcpServer.Type)
			}
			servers = append(servers, mcpServer)
		}
	}

	if verbose {
		fmt.Printf("[registry-resolver] Finished processing. Resolved %d servers total\n", len(servers))
	}

	return servers, nil
}

// resolveRegistryServer fetches a server from the registry and translates it to a runnable config
func resolveRegistryServer(mcpServer common.McpServerType, verbose bool) (*common.McpServerType, error) {
	registryURL := mcpServer.RegistryURL
	if registryURL == "" {
		registryURL = defaultRegistryURL
		if verbose {
			fmt.Printf("[registry-resolver]   Using default registry URL: %s\n", registryURL)
		}
	} else if verbose {
		fmt.Printf("[registry-resolver]   Using specified registry URL: %s\n", registryURL)
	}

	if verbose {
		fmt.Printf("[registry-resolver]   Fetching server %q (version=%q) from registry...\n",
			mcpServer.RegistryServerName, mcpServer.RegistryServerVersion)
	}

	client := registry.NewClient()
	serverEntry, err := client.FetchServer(registryURL, mcpServer.RegistryServerName, mcpServer.RegistryServerVersion)
	if err != nil {
		if verbose {
			fmt.Printf("[registry-resolver]   ERROR: Failed to fetch server from registry: %v\n", err)
		}
		return nil, fmt.Errorf("failed to fetch server %q from registry: %w", mcpServer.RegistryServerName, err)
	}

	if verbose {
		fmt.Printf("[registry-resolver]   Fetched server successfully:\n")
		fmt.Printf("[registry-resolver]     - Name: %s\n", serverEntry.Server.Name)
		fmt.Printf("[registry-resolver]     - Version: %s\n", serverEntry.Server.Version)
		fmt.Printf("[registry-resolver]     - Description: %s\n", serverEntry.Server.Description)
		fmt.Printf("[registry-resolver]     - Packages count: %d\n", len(serverEntry.Server.Packages))
		for j, pkg := range serverEntry.Server.Packages {
			fmt.Printf("[registry-resolver]     - Package[%d]: registryType=%q identifier=%q version=%q\n",
				j, pkg.RegistryType, pkg.Identifier, pkg.Version)
			if len(pkg.EnvironmentVariables) > 0 {
				fmt.Printf("[registry-resolver]       Environment variables: %d\n", len(pkg.EnvironmentVariables))
				for _, envVar := range pkg.EnvironmentVariables {
					fmt.Printf("[registry-resolver]         - %s\n", envVar.Name)
				}
			}
		}
		if len(serverEntry.Server.Remotes) > 0 {
			fmt.Printf("[registry-resolver]     - Remotes count: %d\n", len(serverEntry.Server.Remotes))
			for j, remote := range serverEntry.Server.Remotes {
				fmt.Printf("[registry-resolver]     - Remote[%d]: type=%q url=%q\n",
					j, remote.Type, remote.URL)
			}
		}
	}

	// Collect environment variable overrides from the current environment
	// This allows users to set required env vars before running
	envOverrides := collectEnvOverrides(serverEntry.Server.Packages)

	// Also add user-provided env vars from the manifest (set via TUI or agent.yaml)
	manifestEnvVars := parseManifestEnvVars(mcpServer.Env)
	maps.Copy(envOverrides, manifestEnvVars)

	if verbose {
		if len(envOverrides) > 0 {
			fmt.Printf("[registry-resolver]   Collected %d environment variable overrides (from environment and manifest):\n", len(envOverrides))
			for k, v := range envOverrides {
				// Mask the value for security (unless it's a variable reference)
				maskedValue := v
				if !strings.HasPrefix(v, "${") {
					if len(v) > 4 {
						maskedValue = v[:2] + "***" + v[len(v)-2:]
					} else {
						maskedValue = "***"
					}
				}
				fmt.Printf("[registry-resolver]     - %s=%s\n", k, maskedValue)
			}
		} else {
			fmt.Printf("[registry-resolver]   No environment variable overrides found\n")
		}
	}

	if verbose {
		fmt.Printf("[registry-resolver]   Translating registry server to runnable config (preferRemote=%v)...\n",
			mcpServer.RegistryServerPreferRemote)
	}

	// Translate the registry server spec to a runnable McpServerType
	translated, err := TranslateRegistryServer(mcpServer.Name, &serverEntry.Server, envOverrides, mcpServer.RegistryServerPreferRemote)
	if err != nil {
		if verbose {
			fmt.Printf("[registry-resolver]   ERROR: Failed to translate server: %v\n", err)
		}
		return nil, err
	}

	if verbose {
		fmt.Printf("[registry-resolver]   Translation successful:\n")
		fmt.Printf("[registry-resolver]     - Result type: %s\n", translated.Type)
		if translated.Build == "" {
			fmt.Printf("[registry-resolver]     - Build path: (none - OCI image ready to use)\n")
		} else {
			fmt.Printf("[registry-resolver]     - Build path: %s\n", translated.Build)
		}
		fmt.Printf("[registry-resolver]     - Image: %s\n", translated.Image)
		fmt.Printf("[registry-resolver]     - Command: %s\n", translated.Command)
		fmt.Printf("[registry-resolver]     - Args: %v\n", translated.Args)
		fmt.Printf("[registry-resolver]     - URL: %s\n", translated.URL)
		if len(translated.Env) > 0 {
			fmt.Printf("[registry-resolver]     - Env vars: %d\n", len(translated.Env))
		}
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

// parseManifestEnvVars parses environment variables from the manifest's Env field.
// The Env field is a []string in "KEY=VALUE" format.
func parseManifestEnvVars(envSlice []string) map[string]string {
	result := make(map[string]string)
	for _, env := range envSlice {
		if idx := strings.Index(env, "="); idx > 0 {
			key := env[:idx]
			value := env[idx+1:]
			result[key] = value
		}
	}
	return result
}
