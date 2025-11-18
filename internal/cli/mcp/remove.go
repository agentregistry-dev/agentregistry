package mcp

import (
	"fmt"

	"github.com/spf13/cobra"
)

var RemoveCmd = &cobra.Command{
	Use:           "remove <server-name>",
	Short:         "Remove a deployed MCP server",
	Long:          `Remove a deployed MCP server from the runtime.`,
	Args:          cobra.ExactArgs(1),
	RunE:          runRemove,
	SilenceUsage:  true,  // Don't show usage on removal errors
	SilenceErrors: false, // Still show error messages
}

func runRemove(cmd *cobra.Command, args []string) error {
	serverName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Remove server via API (server will handle reconciliation)
	fmt.Printf("Removing %s from deployments...\n", serverName)
	err := apiClient.RemoveServer(serverName)
	if err != nil {
		return fmt.Errorf("failed to remove server: %w", err)
	}

	fmt.Printf("\n✓ Removed %s\n", serverName)
	fmt.Println("Server removal recorded. The registry will reconcile containers automatically.")

	return nil
}
