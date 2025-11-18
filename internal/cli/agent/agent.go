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
	Use: "agent",
}

func init() {
	AgentCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	AgentCmd.AddCommand(InitCmd)
	AgentCmd.AddCommand(BuildCmd)
	AgentCmd.AddCommand(RunCmd)
}
