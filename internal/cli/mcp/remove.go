package mcp

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/spf13/cobra"
)

var (
	removeVersion string
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

func init() {
	RemoveCmd.Flags().StringVar(&removeVersion, "version", "", "Specify the version of the deployment to remove (for validation)")
}

func runRemove(cmd *cobra.Command, args []string) error {
	apiClient, err := utils.EnsureRegistryConnection()
	if err != nil {
		return err
	}

	serverName := args[0]

	if removeVersion == "" {
		return fmt.Errorf("version is required")
	}

	isDeployed, _ := isServerDeployed(apiClient, serverName, removeVersion)
	if !isDeployed {
		return fmt.Errorf("server %s version %s is not deployed", serverName, removeVersion)
	}

	// Remove server via API (server will handle reconciliation)
	fmt.Printf("Removing %s from deployments...\n", serverName)
	err = apiClient.RemoveServer(serverName, removeVersion)
	if err != nil {
		return fmt.Errorf("failed to remove server %s version %s: %w", serverName, removeVersion, err)
	}

	fmt.Printf("\n✓ Removed %s version %s\n", serverName, removeVersion)
	fmt.Println("Server removal recorded. The registry will reconcile containers automatically.")

	return nil
}
