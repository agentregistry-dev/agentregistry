package cli

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// findServersByName finds servers by name, checking full name first, then partial name
func findServersByName(searchName string) []*v0.ServerResponse {
	servers, err := APIClient.GetServers()
	if err != nil {
		log.Fatalf("Failed to get servers: %v", err)
	}

	// First, try exact match with full name
	for _, s := range servers {
		if s.Server.Name == searchName {
			return []*v0.ServerResponse{s}
		}
	}

	// If no exact match, search for name part (after /)
	var matches []*v0.ServerResponse
	searchLower := strings.ToLower(searchName)

	for _, s := range servers {
		// Extract name part (after /)
		parts := strings.Split(s.Server.Name, "/")
		var namePart string
		if len(parts) == 2 {
			namePart = strings.ToLower(parts[1])
		} else {
			namePart = strings.ToLower(s.Server.Name)
		}

		if namePart == searchLower {
			serverCopy := s
			matches = append(matches, serverCopy)
		}
	}

	return matches
}

// splitServerName splits a server name into namespace and name parts
func splitServerName(fullName string) (namespace, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", fullName
}

// parseKeyValuePairs parses key=value pairs from command line flags
func parseKeyValuePairs(pairs []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, pair := range pairs {
		idx := findFirstEquals(pair)
		if idx == -1 {
			return nil, fmt.Errorf("invalid key=value pair (missing =): %s", pair)
		}
		key := pair[:idx]
		value := pair[idx+1:]
		result[key] = value
	}
	return result, nil
}

// findFirstEquals finds the first = character in a string
func findFirstEquals(s string) int {
	for i, c := range s {
		if c == '=' {
			return i
		}
	}
	return -1
}

// generateRandomName generates a random hex string for use in naming
func generateRandomName() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random name: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

// generateRuntimePaths generates random names and paths for runtime directories
// Returns projectName, runtimeDir, and any error encountered
func generateRuntimePaths(prefix string) (projectName string, runtimeDir string, err error) {
	// Generate a random name
	randomName, err := generateRandomName()
	if err != nil {
		return "", "", err
	}

	// Create project name with prefix
	projectName = prefix + randomName

	// Get home directory and construct runtime directory path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get home directory: %w", err)
	}
	baseRuntimeDir := filepath.Join(homeDir, ".arctl", "runtime")
	runtimeDir = filepath.Join(baseRuntimeDir, prefix+randomName)

	return projectName, runtimeDir, nil
}

// selectServerVersion handles server version selection logic with interactive prompts
// Returns the selected server or an error if not found or cancelled
func selectServerVersion(resourceName, requestedVersion string, autoYes bool) (*v0.ServerResponse, error) {
	if APIClient == nil {
		return nil, fmt.Errorf("API client not initialized")
	}

	// If a specific version was requested, try to get that version
	if requestedVersion != "" && requestedVersion != "latest" {
		fmt.Printf("Checking if MCP server '%s' version '%s' exists in registry...\n", resourceName, requestedVersion)
		server, err := APIClient.GetServerByNameAndVersion(resourceName, requestedVersion)
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
	fmt.Printf("Checking if MCP server '%s' exists in registry...\n", resourceName)
	versions, err := APIClient.GetServerVersions(resourceName)
	if err != nil {
		return nil, fmt.Errorf("error querying registry: %w", err)
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("MCP server '%s' not found in registry. Use 'arctl list mcp' to see available servers", resourceName)
	}

	// Get the latest version (first in the list, as they're ordered by date)
	latestServer := &versions[0]

	// If there are multiple versions, prompt the user (unless --yes is set)
	if len(versions) > 1 {
		fmt.Printf("✓ Found %d versions of MCP server '%s':\n", len(versions), resourceName)
		for i, v := range versions {
			marker := ""
			if i == 0 {
				marker = " (latest)"
			}
			fmt.Printf("  - %s%s\n", v.Server.Version, marker)
		}
		fmt.Printf("\nDefault: version %s (latest)\n", latestServer.Server.Version)

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
			fmt.Println("Auto-accepting latest version (--yes flag set)")
		}
	} else {
		// Only one version available
		fmt.Printf("✓ Found MCP server: %s (version %s)\n", latestServer.Server.Name, latestServer.Server.Version)
	}

	return latestServer, nil
}
