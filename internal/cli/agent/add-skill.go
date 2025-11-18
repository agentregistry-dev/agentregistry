package agent

import (
	"fmt"

	"github.com/spf13/cobra"
)

var AddSkillCmd = &cobra.Command{
	Use:   "add-skill [name] [args...]",
	Short: "Add an skill to agent",
	Long:  `Add an skill to agent. Use flags for non-interactive setup or run without flags to open the wizard.`,
	Args:  cobra.ArbitraryArgs,
	RunE:  runAddSkill,
}

func runAddSkill(cmd *cobra.Command, args []string) error {
	// Not implemented yet
	fmt.Println("Not implemented yet")
	return nil
}
