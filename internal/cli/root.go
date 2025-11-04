package cli

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "AI Registry and Runtime",
	Long:  `arctl is a CLI tool for managing MCP servers, skills, and registries.`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Global flags can be added here
}
