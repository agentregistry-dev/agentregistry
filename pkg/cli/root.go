package cli

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent"
	"github.com/agentregistry-dev/agentregistry/internal/cli/configure"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp"
	"github.com/agentregistry-dev/agentregistry/internal/cli/skill"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/daemon"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/spf13/cobra"
)

// CLIOptions configures the CLI behavior
// We could extend this to include more extensibility options in the future (e.g. client factory)
type CLIOptions struct {
	// DaemonManager handles daemon lifecycle. If nil, uses default.
	DaemonManager types.DaemonManager
}

var cliOptions CLIOptions

// Configure applies options to the root command
func Configure(opts CLIOptions) {
	cliOptions = opts
}

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "Agent Registry CLI",
	Long:  `arctl is a CLI tool for managing agents, MCP servers and skills.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		dm := cliOptions.DaemonManager
		if dm == nil {
			dm = daemon.NewDaemonManager(nil)
		}

		// Check if docker compose is available
		if !utils.IsDockerComposeAvailable() {
			fmt.Println("Docker compose is not available. Please install docker compose and try again.")
			fmt.Println("See https://docs.docker.com/compose/install/ for installation instructions.")
			fmt.Println("agent registry uses docker compose to start the server and the agent gateway.")
			return fmt.Errorf("docker compose is not available")
		}
		if !dm.IsRunning() {
			if err := dm.Start(); err != nil {
				return fmt.Errorf("failed to start daemon: %w", err)
			}
		}
		// Check if local registry is running
		c, err := client.NewClientFromEnv()
		if err != nil {
			return fmt.Errorf("API client not initialized: %w", err)
		}
		APIClient = c
		mcp.SetAPIClient(APIClient)
		agent.SetAPIClient(APIClient)
		skill.SetAPIClient(APIClient)
		cli.SetAPIClient(APIClient)
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
	rootCmd.AddCommand(agent.AgentCmd)
	rootCmd.AddCommand(skill.SkillCmd)
	rootCmd.AddCommand(configure.ConfigureCmd)
	rootCmd.AddCommand(cli.VersionCmd)
	rootCmd.AddCommand(cli.ImportCmd)
	rootCmd.AddCommand(cli.ExportCmd)
}

func Root() *cobra.Command {
	return rootCmd
}
