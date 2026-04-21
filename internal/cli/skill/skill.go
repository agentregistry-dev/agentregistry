package skill

import (
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
)

var verbose bool
var apiClient *client.Client

func SetAPIClient(client *client.Client) {
	apiClient = client
}

var SkillCmd = &cobra.Command{
	Use:     "skill",
	Short:   "Commands for managing skills",
	Long:    `Commands for managing skills.`,
	Args:    cobra.ArbitraryArgs,
	Example: `arctl skill pull my-skill`,
}

func init() {
	SkillCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	SkillCmd.AddCommand(PullCmd)
}
