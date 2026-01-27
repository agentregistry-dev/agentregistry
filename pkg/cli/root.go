package cli

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
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
var registryURL string
var registryToken string

const defaultRegistryPort = "12121"

// Configure applies options to the root command
func Configure(opts CLIOptions) {
	cliOptions = opts
}

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "Agent Registry CLI",
	Long:  `arctl is a CLI tool for managing agents, MCP servers and skills.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		baseURL, token := resolveRegistryTarget()

		dm := cliOptions.DaemonManager
		if dm == nil {
			dm = daemon.NewDaemonManager(nil)
		}

		if shouldAutoStartDaemon(baseURL) {
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
		}

		c, err := client.NewClientWithConfig(baseURL, token)
		if err != nil {
			return fmt.Errorf("API client not initialized: %w", err)
		}
		APIClient = c
		mcp.SetAPIClient(APIClient)
		agent.SetAPIClient(APIClient)
		agentutils.SetDefaultRegistryURL(APIClient.BaseURL)
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
	envBaseURL := os.Getenv("ARCTL_API_BASE_URL")
	envToken := os.Getenv("ARCTL_API_TOKEN")
	rootCmd.PersistentFlags().StringVar(&registryURL, "registry-url", envBaseURL, "Registry base URL (overrides ARCTL_API_BASE_URL; default http://localhost:12121)")
	rootCmd.PersistentFlags().StringVar(&registryToken, "registry-token", envToken, "Registry bearer token (overrides ARCTL_API_TOKEN)")

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

func resolveRegistryTarget() (string, string) {
	base := strings.TrimSpace(registryURL)
	if base == "" {
		base = strings.TrimSpace(os.Getenv("ARCTL_API_BASE_URL"))
	}
	base = normalizeBaseURL(base)

	token := registryToken
	if token == "" {
		token = os.Getenv("ARCTL_API_TOKEN")
	}

	return base, token
}

func normalizeBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return client.DefaultBaseURL
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	return "http://" + trimmed
}

func shouldAutoStartDaemon(targetURL string) bool {
	parsed := parseURL(targetURL)
	if parsed == nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return port == defaultRegistryPort
}

func parseURL(raw string) *url.URL {
	if strings.TrimSpace(raw) == "" {
		raw = client.DefaultBaseURL
	}
	parsed, err := url.Parse(raw)
	if err == nil && parsed.Hostname() != "" {
		return parsed
	}
	parsed, err = url.Parse("http://" + raw)
	if err != nil {
		return nil
	}
	return parsed
}
