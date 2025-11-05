package cli

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "AI Registry and Runtime",
	Long:  `arctl is a CLI tool for managing MCP servers, skills, and registries.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		c, err := client.NewClientFromEnv()
		if err != nil {
			return fmt.Errorf("API client not initialized: %w", err)
		}
		APIClient = c
		return nil
	},
}

// APIClient is the shared API client used by CLI commands
var APIClient *client.Client

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
