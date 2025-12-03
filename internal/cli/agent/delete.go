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
The agent must not be published or deployed unless --force is used.

Examples:
  arctl agent delete my-agent --version 1.0.0
  arctl agent delete my-agent --version 1.0.0 --force`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	DeleteCmd.Flags().StringVar(&deleteVersion, "version", "", "Specify the version to delete (required)")
	DeleteCmd.Flags().BoolVar(&deleteForceFlag, "force", false, "Force delete even if published or deployed")
	_ = DeleteCmd.MarkFlagRequired("version")
}

func runDelete(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Check if agent is published
	isPublished, err := isAgentPublished(agentName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if agent is published: %w", err)
	}

	// Check if agent is deployed
	isDeployed, err := isAgentDeployed(agentName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if agent is deployed: %w", err)
	}

	// Fail if published or deployed unless --force is used
	if !deleteForceFlag {
		if isPublished {
			return fmt.Errorf("agent %s version %s is published. Unpublish it first using 'arctl agent unpublish %s --version %s', or use --force to delete anyway", agentName, deleteVersion, agentName, deleteVersion)
		}
		if isDeployed {
			return fmt.Errorf("agent %s version %s is deployed. Remove it first using 'arctl agent remove %s --version %s', or use --force to delete anyway", agentName, deleteVersion, agentName, deleteVersion)
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

func isAgentPublished(agentName, version string) (bool, error) {
	if apiClient == nil {
		return false, fmt.Errorf("API client not initialized")
	}

	// Get agent using admin endpoint to check published status
	agent, err := apiClient.GetAgentByNameAndVersionAdmin(agentName, version)
	if err != nil {
		return false, err
	}
	if agent == nil {
		return false, nil
	}

	// Check if published field is true
	if agent.Meta.Official != nil && agent.Meta.Official.Published {
		return true, nil
	}

	return false, nil
}

func isAgentDeployed(agentName, version string) (bool, error) {
	if apiClient == nil {
		return false, fmt.Errorf("API client not initialized")
	}

	deployment, err := apiClient.GetDeployedServerByNameAndVersion(agentName, version)
	if err != nil {
		return false, fmt.Errorf("failed to get deployment: %w", err)
	}
	return deployment != nil, nil
}
