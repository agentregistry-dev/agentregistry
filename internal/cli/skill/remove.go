package skill

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/spf13/cobra"
)

var RemoveCmd = &cobra.Command{
	Use:   "remove <skill-name>",
	Short: "Remove a skill",
	Long:  `Remove a skill.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runRemove,
}

func runRemove(cmd *cobra.Command, args []string) error {
	_, err := utils.EnsureRegistryConnection()
	if err != nil {
		return err
	}

	// Not implemented yet
	fmt.Println("Not implemented yet")
	return nil
}
