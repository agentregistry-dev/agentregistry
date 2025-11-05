package cli

import (
	"github.com/agentregistry-dev/agentregistry/internal/registry"
	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the arctl server and all installed MCP servers",
	Long:  `Starts/restarts the arctl server and all installed MCP servers using docker-compose.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return registry.App(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
