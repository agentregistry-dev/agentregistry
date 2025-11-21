package skill

import (
	"github.com/spf13/cobra"
)

var verbose bool

var SkillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Commands for managing skills",
	Long:  `Commands for managing skills.`,
	Args:  cobra.ArbitraryArgs,
	Example: `arctl skill list
arctl skill show my-skill
arctl skill publish ./my-skill
arctl skill remove my-skill`,
}

func init() {
	SkillCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	SkillCmd.AddCommand(InitCmd)
	SkillCmd.AddCommand(ListCmd)
	SkillCmd.AddCommand(PublishCmd)
	SkillCmd.AddCommand(PullCmd)
	SkillCmd.AddCommand(ShowCmd)
	SkillCmd.AddCommand(RemoveCmd)
	SkillCmd.AddCommand(UnpublishCmd)
}
