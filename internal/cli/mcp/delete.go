package mcp

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	deleteVersion string
)

var DeleteCmd = &cobra.Command{
	Use:   "delete <server-name>",
	Short: "Delete an MCP server from the registry",
	Long: `Delete a published MCP server from the registry.

Examples:
  arctl mcp delete my-server --version 1.0.0`,
	Args:          cobra.ExactArgs(1),
	SilenceUsage:  true,  // Don't show usage on removal errors
	SilenceErrors: false, // Still show error messages
	RunE:          runDelete,
}

func init() {
	DeleteCmd.Flags().StringVar(&deleteVersion, "version", "", "Specify the version to delete (required)")
	_ = DeleteCmd.MarkFlagRequired("version")
}

func runDelete(cmd *cobra.Command, args []string) error {
	serverName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Check if server is published
	isPublished, err := isServerPublished(serverName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if server is published: %w", err)
	}

	if !isPublished {
		return fmt.Errorf("server %s version %s not found in registry", serverName, deleteVersion)
	}

	// Delete the server
	fmt.Printf("Deleting server %s version %s...\n", serverName, deleteVersion)
	err = apiClient.DeleteMCPServer(serverName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}

	fmt.Printf("MCP server '%s' version %s deleted successfully\n", serverName, deleteVersion)
	return nil
}
