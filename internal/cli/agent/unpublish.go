package agent

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/spf13/cobra"
)

var UnpublishCmd = &cobra.Command{
	Use:   "unpublish [name] [args...]",
	Short: "Unpublish an agent",
	Long:  `Unpublish an agent. Use flags for non-interactive setup or run without flags to open the wizard.`,
	Args:  cobra.ArbitraryArgs,
	RunE:  runUnpublish,
}

func runUnpublish(cmd *cobra.Command, args []string) error {
	_, err := utils.EnsureRegistryConnection()
	if err != nil {
		return err
	}

	// Not implemented yet
	fmt.Println("Not implemented yet")
	return nil
}
