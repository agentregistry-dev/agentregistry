package agent

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var (
	deleteVersion string
)

var DeleteCmd = &cobra.Command{
	Use:   "delete <agent-name>",
	Short: "Delete an agent from the registry",
	Long: `Delete an agent from the registry.

Examples:
  arctl agent delete my-agent --version 1.0.0`,
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
	agentName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Check if agent exists in registry
	exists, err := isAgentPublished(agentName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if agent exists: %w", err)
	}

	if !exists {
		return fmt.Errorf("agent %s version %s not found in registry", agentName, deleteVersion)
	}

	// Delete the agent
	printer.PrintInfo(fmt.Sprintf("Deleting agent %s version %s...", agentName, deleteVersion))
	if err := apiClient.DeleteAgent(agentName, deleteVersion); err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("Deleted: %s (v%s)", agentName, deleteVersion))
	return nil
}

// isAgentPublished checks if an agent exists in the registry (all entries are visible)
func isAgentPublished(agentName, version string) (bool, error) {
	if apiClient == nil {
		return false, fmt.Errorf("API client not initialized")
	}

	agent, err := apiClient.GetAgentByNameAndVersion(agentName, version)
	if err != nil {
		return false, err
	}
	return agent != nil, nil
}
