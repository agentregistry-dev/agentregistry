package mcp

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
)

var (
	unpublishVersion string
	unpublishAll     bool
)

var UnpublishCmd = &cobra.Command{
	Use:   "unpublish <server-name>",
	Short: "Unpublish an MCP server",
	Long: `Unpublish an MCP server from the registry.

This marks the server as unpublished, hiding it from public listings.
The server data is not deleted and can be re-published later.

Use --all to unpublish all versions of the server.`,
	Args: cobra.ExactArgs(1),
	RunE: runUnpublish,
}

func init() {
	UnpublishCmd.Flags().StringVar(&unpublishVersion, "version", "", "Specify the version of the server to unpublish (defaults to latest)")
	UnpublishCmd.Flags().BoolVar(&unpublishAll, "all", false, "Unpublish all versions of the server")
}

func runUnpublish(cmd *cobra.Command, args []string) error {
	apiClient, err := utils.EnsureRegistryConnection()
	if err != nil {
		return err
	}

	serverName := args[0]

	// Validate flags
	if unpublishAll && unpublishVersion != "" {
		return fmt.Errorf("cannot specify both --all and --version flags")
	}

	// If --all flag is set, unpublish all versions
	if unpublishAll {
		return unpublishAllVersions(apiClient, serverName)
	}

	if unpublishVersion == "" {
		return fmt.Errorf("version is required")
	}

	// Check if the server with the specific version is even published
	isPublished, _ := isServerPublished(apiClient, serverName, unpublishVersion)
	if !isPublished {
		return fmt.Errorf("server %s version %s is not published", serverName, unpublishVersion)
	}

	// Confirm unpublish action
	fmt.Printf("Unpublishing server: %s (version %s)\n", serverName, unpublishVersion)

	// Call the unpublish API
	if err := apiClient.UnpublishMCPServer(serverName, unpublishVersion); err != nil {
		return fmt.Errorf("failed to unpublish server: %w", err)
	}

	fmt.Printf("✓ Successfully unpublished %s version %s\n", serverName, unpublishVersion)
	fmt.Println("\nThe server has been hidden from public listings.")
	fmt.Println("To re-publish it, use: arctl mcp publish")

	return nil
}

func unpublishAllVersions(apiClient *client.Client, serverName string) error {
	fmt.Printf("Fetching all versions of %s...\n", serverName)

	// Get all versions of the server
	versions, err := apiClient.GetServerVersions(serverName)
	if err != nil {
		return fmt.Errorf("failed to get server versions: %w", err)
	}

	if len(versions) == 0 {
		return fmt.Errorf("no versions found for server: %s", serverName)
	}

	fmt.Printf("Found %d version(s)\n\n", len(versions))

	// Unpublish each version
	var failed []string
	var succeeded []string
	for _, version := range versions {
		fmt.Printf("Unpublishing %s version %s...", serverName, version.Server.Version)
		if err := apiClient.UnpublishMCPServer(serverName, version.Server.Version); err != nil {
			fmt.Printf(" ✗ Failed: %v\n", err)
			failed = append(failed, version.Server.Version)
		} else {
			fmt.Printf(" ✓\n")
			succeeded = append(succeeded, version.Server.Version)
		}
	}

	// Print summary
	fmt.Println()
	if len(succeeded) > 0 {
		fmt.Printf("✓ Successfully unpublished %d version(s)\n", len(succeeded))
	}
	if len(failed) > 0 {
		fmt.Printf("✗ Failed to unpublish %d version(s)\n", len(failed))
		for _, v := range failed {
			fmt.Printf("  - %s\n", v)
		}
		return fmt.Errorf("some versions failed to unpublish")
	}

	fmt.Println("\nAll versions have been hidden from public listings.")
	fmt.Println("To re-publish them, use: arctl mcp publish")

	return nil
}
