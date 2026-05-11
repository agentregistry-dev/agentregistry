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

// selectServerTag handles server tag selection logic with interactive prompts.
// Returns the selected server or an error if not found or cancelled
func selectServerTag(resourceName, requestedTag string, autoYes bool) (*v1alpha1.MCPServer, error) {
	if apiClient == nil {
		return nil, errors.New("API client not initialized")
	}

	// If a specific tag was requested, try to get that tag.
	if requestedTag != "" && requestedTag != "latest" {
		fmt.Printf("Checking if MCP server '%s' tag '%s' exists in registry...\n", resourceName, requestedTag)
		server, err := client.GetTyped(
			context.Background(),
			apiClient,
			v1alpha1.KindMCPServer,
			v1alpha1.DefaultNamespace,
			resourceName,
			requestedTag,
			func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
		)
		if err != nil {
			return nil, fmt.Errorf("error querying registry: %w", err)
		}
		if server == nil {
			return nil, fmt.Errorf("MCP server '%s' tag '%s' not found in registry", resourceName, requestedTag)
		}

		fmt.Printf("✓ Found MCP server: %s (tag %s)\n", server.Metadata.Name, server.Metadata.Tag)
		return server, nil
	}

	// No specific tag requested, check all tags.
	fmt.Printf("Checking for tags of MCP server '%s'...\n", resourceName)
	allTags, err := client.ListTagsOfName(
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

	if len(allTags) == 0 {
		return nil, fmt.Errorf("MCP server '%s' not found in registry. Use 'arctl get mcpservers' to see available servers", resourceName)
	}

	// If there are multiple tags, prompt the user (unless --yes is set)
	if len(allTags) > 1 { //nolint:nestif
		fmt.Printf("✓ Found %d tag(s) of MCP server '%s':\n", len(allTags), resourceName)
		for i, v := range allTags {
			marker := ""
			if i == 0 {
				marker = " (latest)"
			}
			fmt.Printf("  - %s%s\n", v.Metadata.Tag, marker)
		}

		// Skip prompt if --yes flag is set
		if !autoYes {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Proceed with the latest tag? [Y/n]: ")
			response, err := reader.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("error reading input: %w", err)
			}

			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				return nil, fmt.Errorf("operation cancelled. To use a specific tag, use: --tag <tag>")
			}
		} else {
			fmt.Println("Auto-accepting latest tag (--yes flag set)")
		}
	} else {
		fmt.Printf("✓ Found MCP server: %s (tag %s)\n", allTags[0].Metadata.Name, allTags[0].Metadata.Tag)
	}

	return allTags[0], nil
}
