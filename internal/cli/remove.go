package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRemoveCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <resource-type> <resource-name>",
		Short: "Remove a deployed resource",
		Long:  `Remove deployed resources (mcp server, skill, agent) from the runtime.`,
		Args:  cobra.ExactArgs(2),
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Not implemented yet")
		},
	}
}

func init() {
	rootCmd.AddCommand(newRemoveCommand())
}
