package mcp

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/models"
)

func isServerDeployed(serverName string, version string) (bool, error) {
	if apiClient == nil {
		return false, errors.New("API client not initialized")
	}

	deployment, err := apiClient.GetDeployedServerByNameAndVersion(serverName, version)
	if err != nil {
		return false, fmt.Errorf("failed to get deployment: %w", err)
	}
	return deployment != nil, nil
}

// isServerPublished checks if a server is published
func isServerPublished(serverName, version string) (bool, error) {
	if apiClient == nil {
		return false, errors.New("API client not initialized")
	}

	// Get all versions (admin endpoint) to check published status
	allVersions, err := apiClient.GetAllServerVersionsAdmin(serverName)
	if err != nil {
		return false, err
	}

	// Find the specific version and check if it's published
	for _, v := range allVersions {
		if v.Server.Version == version {
			// Check if published field exists in metadata
			// Note: The published field is stored in the database but may not be in the response
			// For now, if we can get it with publishedOnly=true, it's published
			publishedServer, err := apiClient.GetServerByNameAndVersion(serverName, version, true, false)
			if err != nil {
				return false, err
			}
			return publishedServer != nil, nil
		}
	}

	return false, nil
}

// isServerApproved checks if a server is approved
func isServerApproved(serverName, version string) (bool, error) {
	if apiClient == nil {
		return false, errors.New("API client not initialized")
	}

	// Get all versions (admin endpoint) to check approval status
	allVersions, err := apiClient.GetAllServerVersionsAdmin(serverName)
	if err != nil {
		return false, err
	}

	// Find the specific version and check if it's approved
	for _, v := range allVersions {
		if v.Server.Version == version {
			// The approval status should be stored in the database and returned in the response
			return v.Meta.ApprovalStatus.Status == "APPROVED", nil
		}
	}

	return false, nil
}

// selectServerVersion handles server version selection logic with interactive prompts
// Returns the selected server or an error if not found or cancelled
// Only allows deployment of published-approved servers
func selectServerVersion(resourceName, requestedVersion string, autoYes bool) (*models.ServerResponse, error) {
	if apiClient == nil {
		return nil, errors.New("API client not initialized")
	}

	// If a specific version was requested, try to get that version
	if requestedVersion != "" && requestedVersion != "latest" {
		fmt.Printf("Checking if MCP server '%s' version '%s' exists in registry...\n", resourceName, requestedVersion)
		server, err := apiClient.GetServerByNameAndVersion(resourceName, requestedVersion, true, true)
		if err != nil {
			return nil, fmt.Errorf("error querying registry: %w", err)
		}
		if server == nil {
			return nil, fmt.Errorf("MCP server '%s' version '%s' not found in registry", resourceName, requestedVersion)
		}

		// Check if the server is published
		isPublished, err := isServerPublished(server.Server.Name, server.Server.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to check if server is published: %w", err)
		}
		if !isPublished {
			return nil, fmt.Errorf("cannot deploy unpublished server %s (version %s). Use 'arctl mcp publish %s' to publish it first",
				resourceName, requestedVersion, resourceName)
		}

		// Check if the server is approved
		isApproved, err := isServerApproved(server.Server.Name, server.Server.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to check if server is approved: %w", err)
		}
		if !isApproved {
			return nil, fmt.Errorf("cannot deploy unapproved server %s (version %s). It must be approved by an admin first",
				resourceName, requestedVersion)
		}

		fmt.Printf("✓ Found MCP server: %s (version %s)\n", server.Server.Name, server.Server.Version)
		return server, nil
	}

	// No specific version requested, check all versions
	fmt.Printf("Checking for published versions of MCP server '%s'...\n", resourceName)
	allVersions, err := apiClient.GetServerVersions(resourceName)
	if err != nil {
		return nil, fmt.Errorf("error querying registry: %w", err)
	}

	if len(allVersions) == 0 {
		return nil, fmt.Errorf("MCP server '%s' not found in registry. Use 'arctl mcp list' to see available servers", resourceName)
	}

	// Filter to only published and approved versions
	var publishedVersions []*models.ServerResponse
	for _, v := range allVersions {
		isPublished, err := isServerPublished(resourceName, v.Server.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to check if server is published: %w", err)
		}
		if !isPublished {
			continue
		}

		isApproved, err := isServerApproved(resourceName, v.Server.Version)
		if err != nil {
			return nil, fmt.Errorf("failed to check if server is approved: %w", err)
		}
		if isApproved {
			vCopy := v
			publishedVersions = append(publishedVersions, &vCopy)
		}
	}

	// If there are multiple published versions, prompt the user (unless --yes is set)
	if len(publishedVersions) > 1 {
		fmt.Printf("✓ Found %d published version(s) of MCP server '%s':\n", len(allVersions), resourceName)
		for i, v := range publishedVersions {
			marker := ""
			if i == 0 {
				marker = " (latest)"
			}
			fmt.Printf("  - %s%s\n", v.Server.Version, marker)
		}

		// Skip prompt if --yes flag is set
		if !autoYes {
			// Prompt user for confirmation
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
			fmt.Println("Auto-accepting latest published version (--yes flag set)")
		}
	} else {
		// Only one published version available
		fmt.Printf("✓ Found published MCP server: %s (version %s)\n", publishedVersions[0].Server.Name, publishedVersions[0].Server.Version)
	}

	return publishedVersions[0], nil
}
