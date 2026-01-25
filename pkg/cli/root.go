package cli

import (
	"errors"
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

	// AuthnProvider provides CLI-specific authentication.
	// If nil, uses ARCTL_API_TOKEN env var.
	AuthnProvider types.CLIAuthnProvider
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

		// Get authentication token
		var token string
		if cliOptions.AuthnProvider != nil {
			var err error
			token, err = cliOptions.AuthnProvider.Authenticate(cmd.Context())
			if err != nil {
				// missing stored token is a sentinel error, and should not block all commands (e.g. artifact init)
				if errors.Is(err, types.ErrCLINoStoredToken) {
					if verbose {
						fmt.Println("No stored authentication token found. Continuing without authentication.")
					}
				}
				// in this case, there is a valid issue with authentication, so block the command execution
				return fmt.Errorf("CLI authentication failed: %w", err)
			}
		}

		// Check if local registry is running and create API client
		var c *client.Client
		var err error
		if token != "" {
			// Use token from custom provider
			baseURL := os.Getenv("ARCTL_API_BASE_URL")
			if baseURL == "" {
				baseURL = "http://localhost:12121/v0"
			}
			c = client.NewClient(baseURL, token)
			// Verify connectivity
			if err := c.Ping(); err != nil {
				return fmt.Errorf("failed to reach API: %w", err)
			}
		} else {
			// Use default env-based client (existing behavior)
			c, err = client.NewClientFromEnv()
			if err != nil {
				return fmt.Errorf("API client not initialized: %w", err)
			}
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
	rootCmd.AddCommand(cli.EmbeddingsCmd)
}

func Root() *cobra.Command {
	return rootCmd
}
