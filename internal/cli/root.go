package cli

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/daemon"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "AI Registry and Runtime",
	Long:  `arctl is a CLI tool for managing MCP servers, skills, and registries.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Check if docker compose is available
		if !daemon.IsDockerComposeAvailable() {
			fmt.Println("Docker compose is not available. Please install docker compose and try again.")
			fmt.Println("See https://docs.docker.com/compose/install/ for installation instructions.")
			fmt.Println("agent registry uses docker compose to start the server and the agent gateway.")
			return fmt.Errorf("docker compose is not available")
		}
		if !daemon.IsRunning() {
			if err := daemon.Start(); err != nil {
				return fmt.Errorf("failed to start daemon: %w", err)
			}
		}
		// Check if local registry is running
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
var verbose bool

func Execute() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false, "Verbose output")
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(mcp.McpCmd)
}
