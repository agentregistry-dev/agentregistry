package agent

import (
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// TODO: add unit tests for this file.

// hasRegistryServers checks if the manifest has any registry-type MCP servers.
func hasRegistryServers(manifest *models.AgentManifest) bool {
	for _, srv := range manifest.McpServers {
		if srv.Type == "registry" {
			return true
		}
	}
	return false
}

func resolveMCPServersForRuntime(manifest *models.AgentManifest) ([]models.McpServerType, []common.PythonMCPServer, error) {
	if manifest == nil {
		return nil, nil, fmt.Errorf("agent manifest is required")
	}
	if !hasRegistryServers(manifest) {
		if verbose {
			fmt.Println("[registry-resolve] No registry-type MCP servers found in manifest")
		}
		return nil, nil, nil
	}

	if verbose {
		fmt.Println("[registry-resolve] Detected registry-type MCP servers in manifest")
		fmt.Printf("[registry-resolve] Total MCP servers in manifest: %d\n", len(manifest.McpServers))
		for i, srv := range manifest.McpServers {
			fmt.Printf("[registry-resolve]   [%d] name=%q type=%q registryServerName=%q registryURL=%q\n",
				i, srv.Name, srv.Type, srv.RegistryServerName, srv.RegistryURL)
		}
		fmt.Println("[registry-resolve] Starting resolution of registry servers...")
	}

	servers, err := agentutils.ParseAgentManifestServers(manifest, verbose)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse agent manifest mcp servers: %w", err)
	}
	manifest.McpServers = servers

	if verbose {
		fmt.Printf("[registry-resolve] Resolution complete. Total servers after resolution: %d\n", len(manifest.McpServers))
		for i, srv := range manifest.McpServers {
			fmt.Printf("[registry-resolve]   [%d] name=%q type=%q build=%q image=%q command=%q\n",
				i, srv.Name, srv.Type, srv.Build, srv.Image, srv.Command)
		}
	}

	var registryResolvedServers []models.McpServerType
	for _, srv := range manifest.McpServers {
		if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
			registryResolvedServers = append(registryResolvedServers, srv)
			if verbose {
				fmt.Printf("[registry-resolve] Including server %q for build (type=command, build=%q)\n", srv.Name, srv.Build)
			}
			continue
		}
		if verbose {
			if srv.Type == "command" && srv.Build == "" && srv.Image != "" {
				fmt.Printf("[registry-resolve] Skipping server %q for build (OCI image %q ready to use)\n", srv.Name, srv.Image)
			} else {
				fmt.Printf("[registry-resolve] Skipping server %q for build (type=%q, build=%q)\n", srv.Name, srv.Type, srv.Build)
			}
		}
	}
	if len(registryResolvedServers) > 0 {
		if verbose {
			fmt.Printf("[registry-resolve] %d registry-resolved servers require directory setup\n", len(registryResolvedServers))
		}
	} else if verbose {
		fmt.Println("[registry-resolve] No registry-resolved command servers to build")
	}

	serversForConfig := common.PythonServersFromManifest(manifest)
	if verbose {
		fmt.Printf("[registry-resolve] Created %d server configurations for MCP config (includes OCI servers)\n", len(serversForConfig))
	}

	return registryResolvedServers, serversForConfig, nil
}
