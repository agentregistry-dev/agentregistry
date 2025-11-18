package skill

import (
	"fmt"

	"github.com/spf13/cobra"
)

var DeployCmd = &cobra.Command{
	Use:   "deploy <skill-name>",
	Short: "Deploy a skill",
	Long:  `Deploy a skill.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	// Not implemented yet
	fmt.Println("Not implemented yet")
	return nil
}
