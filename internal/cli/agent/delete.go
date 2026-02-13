package agent

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	deleteForceFlag bool
	deleteVersion   string
)

var DeleteCmd = &cobra.Command{
	Use:   "delete <agent-name>",
	Short: "Delete an agent from the registry",
	Long: `Delete an agent from the registry.
The agent must not be deployed unless --force is used.

Examples:
  arctl agent delete my-agent --version 1.0.0
  arctl agent delete my-agent --version 1.0.0 --force`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	DeleteCmd.Flags().StringVar(&deleteVersion, "version", "", "Specify the version to delete (required)")
	DeleteCmd.Flags().BoolVar(&deleteForceFlag, "force", false, "Force delete even if deployed")
	_ = DeleteCmd.MarkFlagRequired("version")
}

func runDelete(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Check if agent is deployed
	isDeployed, err := isAgentDeployed(agentName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if agent is deployed: %w", err)
	}

	// Fail if deployed unless --force is used
	if !deleteForceFlag && isDeployed {
		return fmt.Errorf("agent %s version %s is deployed. Remove it first using 'arctl agent remove %s --version %s', or use --force to delete anyway", agentName, deleteVersion, agentName, deleteVersion)
	}

	// Make sure to remove the deployment before deleting the agent from database
	if deleteForceFlag && isDeployed {
		if err := apiClient.RemoveDeployment(agentName, deleteVersion, "agent"); err != nil {
			return fmt.Errorf("failed to remove deployment before delete: %w", err)
		}
	}

	// Delete the agent
	fmt.Printf("Deleting agent %s version %s...\n", agentName, deleteVersion)
	err = apiClient.DeleteAgent(agentName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	fmt.Printf("Agent '%s' version %s deleted successfully\n", agentName, deleteVersion)
	return nil
}

func isAgentDeployed(agentName, version string) (bool, error) {
	if apiClient == nil {
		return false, fmt.Errorf("API client not initialized")
	}

	deployment, err := apiClient.GetDeployedServerByNameAndVersion(agentName, version, "agent")
	if err != nil {
		return false, fmt.Errorf("failed to get deployment: %w", err)
	}
	return deployment != nil, nil
}
