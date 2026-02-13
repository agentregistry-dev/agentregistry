package mcp

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/printer"
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
	SilenceUsage:  true,
	SilenceErrors: false,
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

	// Check if server exists in registry
	exists, err := isServerPublished(serverName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if server exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("server %s version %s not found in registry", serverName, deleteVersion)
	}

	// Delete the server
	printer.PrintInfo(fmt.Sprintf("Deleting server %s version %s...", serverName, deleteVersion))
	if err := apiClient.DeleteMCPServer(serverName, deleteVersion); err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("Deleted: %s (v%s)", serverName, deleteVersion))
	return nil
}
