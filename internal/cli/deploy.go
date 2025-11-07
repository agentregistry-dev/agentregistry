package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deployCmd = &cobra.Command{
	Use:   "deploy <resource-type> <resource-name>",
	Short: "Deploy a resource",
	Long:  `Deploy resources (mcp server, skill, agent) to the runtime.`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Not implemented yet")
	},
}

func init() {
	rootCmd.AddCommand(deployCmd)
}
