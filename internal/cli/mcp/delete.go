package mcp

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	deleteForceFlag bool
	deleteVersion   string
)

var DeleteCmd = &cobra.Command{
	Use:   "delete <server-name>",
	Short: "Delete an MCP server from the registry",
	Long: `Delete an MCP server from the registry.
The server must not be published or deployed unless --force is used.

Examples:
  arctl mcp delete my-server --version 1.0.0
  arctl mcp delete my-server --version 1.0.0 --force`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	DeleteCmd.Flags().StringVar(&deleteVersion, "version", "", "Specify the version to delete (required)")
	DeleteCmd.Flags().BoolVar(&deleteForceFlag, "force", false, "Force delete even if published or deployed")
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

	// Check if server is deployed
	isDeployed, err := isServerDeployed(serverName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if server is deployed: %w", err)
	}

	// Fail if published or deployed unless --force is used
	if !deleteForceFlag {
		if isPublished {
			return fmt.Errorf("server %s version %s is published. Unpublish it first using 'arctl mcp unpublish %s --version %s', or use --force to delete anyway", serverName, deleteVersion, serverName, deleteVersion)
		}
		if isDeployed {
			return fmt.Errorf("server %s version %s is deployed. Remove it first using 'arctl mcp remove %s --version %s', or use --force to delete anyway", serverName, deleteVersion, serverName, deleteVersion)
		}
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
