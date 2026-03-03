package prompt

import (
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
)

var verbose bool
var apiClient *client.Client

func SetAPIClient(client *client.Client) {
	apiClient = client
}

var PromptCmd = &cobra.Command{
	Use:   "prompt",
	Short: "Commands for managing prompts",
	Long:  `Commands for managing prompts.`,
	Args:  cobra.ArbitraryArgs,
	Example: `arctl prompt publish system-prompt.txt --name my-prompt --version 1.0.0
arctl prompt list
arctl prompt show my-prompt
arctl prompt delete my-prompt --version 1.0.0`,
}

func init() {
	PromptCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	PromptCmd.AddCommand(ListCmd)
	PromptCmd.AddCommand(PublishCmd)
	PromptCmd.AddCommand(DeleteCmd)
	PromptCmd.AddCommand(ShowCmd)
}
