package agent

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/spf13/cobra"
)

var DeployCmd = &cobra.Command{
	Use:   "deploy [name] [args...]",
	Short: "Deploy an agent",
	Long:  `Deploy an agent. Use flags for non-interactive setup or run without flags to open the wizard.`,
	Args:  cobra.ArbitraryArgs,
	RunE:  runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	_, err := utils.EnsureRegistryConnection()
	if err != nil {
		return err
	}

	// Not implemented yet
	fmt.Println("Not implemented yet")
	return nil
}
