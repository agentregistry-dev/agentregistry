package mcp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// fileExists checks if a file exists at the given path.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// selectServerVersion handles server version selection logic with interactive prompts
// Returns the selected server or an error if not found or cancelled
func selectServerVersion(resourceName, requestedVersion string, autoYes bool) (*v1alpha1.MCPServer, error) {
	if apiClient == nil {
		return nil, errors.New("API client not initialized")
	}

	// If a specific version was requested, try to get that version
	if requestedVersion != "" && requestedVersion != "latest" {
		fmt.Printf("Checking if MCP server '%s' version '%s' exists in registry...\n", resourceName, requestedVersion)
		server, err := client.GetTyped(
			context.Background(),
			apiClient,
			v1alpha1.KindMCPServer,
			v1alpha1.DefaultNamespace,
			resourceName,
			requestedVersion,
			func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
		)
		if err != nil {
			return nil, fmt.Errorf("error querying registry: %w", err)
		}
		if server == nil {
			return nil, fmt.Errorf("MCP server '%s' version '%s' not found in registry", resourceName, requestedVersion)
		}

		fmt.Printf("✓ Found MCP server: %s (version %s)\n", server.Metadata.Name, server.Metadata.Version)
		return server, nil
	}

	// No specific version requested, check all versions
	fmt.Printf("Checking for versions of MCP server '%s'...\n", resourceName)
	allVersions, err := client.ListVersionsOfName(
		context.Background(),
		apiClient,
		v1alpha1.KindMCPServer,
		v1alpha1.DefaultNamespace,
		resourceName,
		func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
	)
	if err != nil {
		return nil, fmt.Errorf("error querying registry: %w", err)
	}

	if len(allVersions) == 0 {
		return nil, fmt.Errorf("MCP server '%s' not found in registry. Use 'arctl get mcpservers' to see available servers", resourceName)
	}

	// If there are multiple versions, prompt the user (unless --yes is set)
	if len(allVersions) > 1 { //nolint:nestif
		fmt.Printf("✓ Found %d version(s) of MCP server '%s':\n", len(allVersions), resourceName)
		for i, v := range allVersions {
			marker := ""
			if i == 0 {
				marker = " (latest)"
			}
			fmt.Printf("  - %s%s\n", v.Metadata.Version, marker)
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
		fmt.Printf("✓ Found MCP server: %s (version %s)\n", allVersions[0].Metadata.Name, allVersions[0].Metadata.Version)
	}

	return allVersions[0], nil
}
