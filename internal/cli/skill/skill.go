package skill

import (
	"github.com/spf13/cobra"
)

var verbose bool

var SkillCmd = &cobra.Command{
	Use: "skill",
}

func init() {
	SkillCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
}
