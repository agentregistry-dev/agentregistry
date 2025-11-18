package agent

import (
	"fmt"

	"github.com/spf13/cobra"
)

var RemoveCmd = &cobra.Command{
	Use:   "remove [name] [args...]",
	Short: "Remove an agent",
	Long:  `Remove an agent that is deployed. Use flags for non-interactive setup or run without flags to open the wizard.`,
	Args:  cobra.ArbitraryArgs,
	RunE:  runRemove,
}

func runRemove(cmd *cobra.Command, args []string) error {
	// Not implemented yet
	fmt.Println("Not implemented yet")
	return nil
}
