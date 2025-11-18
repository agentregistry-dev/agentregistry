package agent

import (
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
)

var verbose bool
var apiClient *client.Client

func SetAPIClient(client *client.Client) {
	apiClient = client
}

var AgentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Commands for managing agents",
	Long:  `Commands for managing agents.`,
	Args:  cobra.ArbitraryArgs,
	Example: `arctl agent list
arctl agent show dice
arctl agent publish ./my-agent
arctl agent remove dice
arctl agent run ./my-agent`,
}

func init() {
	AgentCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	AgentCmd.AddCommand(InitCmd)
	AgentCmd.AddCommand(BuildCmd)
	AgentCmd.AddCommand(RunCmd)
	AgentCmd.AddCommand(AddSkillCmd)
	AgentCmd.AddCommand(AddMcpCmd)
	AgentCmd.AddCommand(PublishCmd)
	AgentCmd.AddCommand(RemoveCmd)
	AgentCmd.AddCommand(DeployCmd)
	AgentCmd.AddCommand(UnpublishCmd)
	AgentCmd.AddCommand(ListCmd)
	AgentCmd.AddCommand(ShowCmd)
}
