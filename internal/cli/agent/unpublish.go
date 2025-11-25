package agent

import (
	"fmt"

	"github.com/spf13/cobra"
)

var UnpublishCmd = &cobra.Command{
	Use:   "unpublish [agent-name]",
	Short: "Unpublish an agent from the registry",
	Long: `Unpublish an agent from the registry by marking it as unpublished.

This command operates only on agents that already exist in the registry.
It sets the published flag to false, hiding the agent from public listings.

Examples:
  arctl agent unpublish my-agent --version latest
  arctl agent unpublish my-agent --version 1.2.3`,
	Args:    cobra.ExactArgs(1),
	RunE:    runUnpublish,
	Example: `arctl agent unpublish my-agent --version latest`,
}

var unpublishVersion string

func init() {
	UnpublishCmd.Flags().StringVar(&unpublishVersion, "version", "latest", "Version of the agent to unpublish")
}

func runUnpublish(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	agentName := args[0]
	version := unpublishVersion
	if version == "" {
		version = "latest"
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Call the admin unpublish status endpoint
	if err := apiClient.UnpublishAgentStatus(agentName, version); err != nil {
		return fmt.Errorf("failed to unpublish agent: %w", err)
	}

	fmt.Printf("Agent '%s' version '%s' unpublished successfully\n", agentName, version)

	return nil
}
