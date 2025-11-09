package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Show version information",
	Long:  `Displays the version of arctl.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("arctl version %s\n", version.Version)
		fmt.Printf("Git commit: %s\n", version.GitCommit)
		fmt.Printf("Build date: %s\n", version.BuildDate)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
