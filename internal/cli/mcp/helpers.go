package mcp

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// isServerPublished checks if a server exists in the registry (all entries are visible)
func isServerPublished(serverName, version string) (bool, error) {
	if apiClient == nil {
		return false, errors.New("API client not initialized")
	}

	server, err := apiClient.GetServerByNameAndVersion(serverName, version)
	if err != nil {
		return false, err
	}
	return server != nil, nil
}

// selectServerVersion handles server version selection logic with interactive prompts
// Returns the selected server or an error if not found or cancelled
func selectServerVersion(resourceName, requestedVersion string, autoYes bool) (*apiv0.ServerResponse, error) {
	if apiClient == nil {
		return nil, errors.New("API client not initialized")
	}

	// If a specific version was requested, try to get that version
	if requestedVersion != "" && requestedVersion != "latest" {
		fmt.Printf("Checking if MCP server '%s' version '%s' exists in registry...\n", resourceName, requestedVersion)
		server, err := apiClient.GetServerByNameAndVersion(resourceName, requestedVersion)
		if err != nil {
			return nil, fmt.Errorf("error querying registry: %w", err)
		}
		if server == nil {
			return nil, fmt.Errorf("MCP server '%s' version '%s' not found in registry", resourceName, requestedVersion)
		}

		fmt.Printf("✓ Found MCP server: %s (version %s)\n", server.Server.Name, server.Server.Version)
		return server, nil
	}

	// No specific version requested, check all versions
	fmt.Printf("Checking for versions of MCP server '%s'...\n", resourceName)
	allVersions, err := apiClient.GetServerVersions(resourceName)
	if err != nil {
		return nil, fmt.Errorf("error querying registry: %w", err)
	}

	if len(allVersions) == 0 {
		return nil, fmt.Errorf("MCP server '%s' not found in registry. Use 'arctl mcp list' to see available servers", resourceName)
	}

	// If there are multiple versions, prompt the user (unless --yes is set)
	if len(allVersions) > 1 { //nolint:nestif
		fmt.Printf("✓ Found %d version(s) of MCP server '%s':\n", len(allVersions), resourceName)
		for i, v := range allVersions {
			marker := ""
			if i == 0 {
				marker = " (latest)"
			}
			fmt.Printf("  - %s%s\n", v.Server.Version, marker)
		}

		// Skip prompt if --yes flag is set
		if !autoYes {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Proceed with the latest version? [Y/n]: ")
			response, err := reader.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("error reading input: %w", err)
			}

			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				return nil, fmt.Errorf("operation cancelled. To use a specific version, use: --version <version>")
			}
		} else {
			fmt.Println("Auto-accepting latest version (--yes flag set)")
		}
	} else {
		fmt.Printf("✓ Found MCP server: %s (version %s)\n", allVersions[0].Server.Name, allVersions[0].Server.Version)
	}

	return &allVersions[0], nil
}
