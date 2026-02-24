package cli

import (
	"context"
	"errors"
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

const defaultRegistryPort = "12121"

// ClientFactory creates an API client for the given base URL and token.
// Used for testing when nil; production uses client.NewClientWithConfig.
type ClientFactory func(ctx context.Context, baseURL, token string) (*client.Client, error)

// CLIOptions configures the CLI behavior.
// Can be extended for more options (e.g. client factory).
type CLIOptions struct {
	// DaemonManager handles daemon lifecycle. If nil, uses default.
	DaemonManager types.DaemonManager

	// AuthnProviderFactory provides CLI-specific authentication.
	AuthnProviderFactory types.CLIAuthnProviderFactory

	// OnTokenResolved is called when a token is resolved.
	// This allows extensions to perform additional actions when a token is resolved (e.g. storing locally).
	OnTokenResolved func(token string) error

	// ClientFactory creates the API client. If nil, uses client.NewClientWithConfig (requires network).
	ClientFactory ClientFactory
}

var (
	cliOptions    CLIOptions
	registryURL   string
	registryToken string
	verbose       bool
)

// APIClient is the shared API client used by CLI commands after pre-run.
var APIClient *client.Client

// Configure applies options to the root command (e.g. for tests or alternate entry points).
func Configure(opts CLIOptions) {
	cliOptions = opts
}

// Root returns the root cobra command. Used by main and tests.
func Root() *cobra.Command {
	return rootCmd
}

// Execute runs the root command with default flags (e.g. verbose) and exits on error.
func Execute() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false, "Verbose output")
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "Agent Registry CLI",
	Long:  `arctl is a CLI tool for managing agents, MCP servers and skills.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		baseURL, token := resolveRegistryTarget(os.Getenv)
		skipSetup, autoStartDaemon := preRunBehavior(cmd, baseURL)
		if skipSetup {
			return nil
		}

		c, err := preRunSetup(cmd.Context(), cmd, baseURL, token, autoStartDaemon)
		if err != nil {
			return err
		}

		APIClient = c
		mcp.SetAPIClient(c)
		agent.SetAPIClient(c)
		agentutils.SetDefaultRegistryURL(c.BaseURL)
		skill.SetAPIClient(c)
		cli.SetAPIClient(c)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&registryURL, "registry-url", os.Getenv("ARCTL_API_BASE_URL"), "Registry base URL (overrides ARCTL_API_BASE_URL; default http://localhost:12121)")
	rootCmd.PersistentFlags().StringVar(&registryToken, "registry-token", os.Getenv("ARCTL_API_TOKEN"), "Registry bearer token (overrides ARCTL_API_TOKEN)")

	rootCmd.AddCommand(mcp.McpCmd)
	rootCmd.AddCommand(agent.AgentCmd)
	rootCmd.AddCommand(skill.SkillCmd)
	rootCmd.AddCommand(configure.ConfigureCmd)
	rootCmd.AddCommand(cli.VersionCmd)
	rootCmd.AddCommand(cli.ImportCmd)
	rootCmd.AddCommand(cli.ExportCmd)
	rootCmd.AddCommand(cli.EmbeddingsCmd)
}

// resolveRegistryTarget returns base URL and token from flags and env.
// getEnv is typically os.Getenv; injected for tests.
func resolveRegistryTarget(getEnv func(string) string) (baseURL, token string) {
	base := strings.TrimSpace(registryURL)
	if base == "" {
		base = strings.TrimSpace(getEnv("ARCTL_API_BASE_URL"))
	}
	base = normalizeBaseURL(base)

	token = registryToken
	if token == "" {
		token = getEnv("ARCTL_API_TOKEN")
	}
	return base, token
}

// resolveAuthToken resolves the authentication token from the CLI authentication provider.
func resolveAuthToken(ctx context.Context, cmd *cobra.Command, factory types.CLIAuthnProviderFactory) (string, error) {
	provider, err := factory(cmd.Root())
	if err != nil {
		if errors.Is(err, types.ErrNoOIDCDefined) {
			return "", nil // non-blocking, user may be running a command that does not require authentication
		}
		return "", fmt.Errorf("failed to create CLI authentication provider: %w", err)
	}
	if provider == nil {
		return "", nil // non-blocking, user may be running a command that does not require authentication
	}

	token, err := provider.Authenticate(ctx)
	if err != nil {
		if errors.Is(err, types.ErrCLINoStoredToken) {
			return "", nil // non-blocking, user may be running a command that does not require authentication
		}
		return "", fmt.Errorf("CLI authentication failed: %w", err)
	}
	return token, nil
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

func parseRegistryURL(raw string) *url.URL {
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

// preRunDaemonBehavior defines which commands skip setup and when to auto-start the daemon.
// Key: parent name; value: set of subcommand names that skip daemon/client setup.
var preRunDaemonBehavior = struct {
	skipCommands map[string]map[string]bool
}{
	skipCommands: map[string]map[string]bool{
		"agent": {"init": true},
		"mcp":   {"init": true},
		"skill": {"init": true},
	},
}

// preRunBehavior returns whether to skip pre-run setup (e.g. agent/mcp/skill init) and
// whether to auto-start the daemon when the registry target is localhost:12121.
func preRunBehavior(cmd *cobra.Command, baseURL string) (skipSetup bool, autoStartDaemon bool) {
	// Skip daemon and client setup for specific commands and any of their subcommand
	if cmd != nil {
		for c := cmd; c != nil; c = c.Parent() {
			parent := c.Parent()
			if parent == nil {
				break
			}
			if subcommands, ok := preRunDaemonBehavior.skipCommands[parent.Name()]; ok && subcommands[c.Name()] {
				return true, false
			}
		}
	}

	// Auto-start daemon only for localhost on default registry port
	parsed := parseRegistryURL(baseURL)
	if parsed == nil {
		return false, false
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return false, false
	}
	port := parsed.Port()
	if port == "" {
		if parsed.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	autoStartDaemon = (port == defaultRegistryPort)
	return false, autoStartDaemon
}

// preRunSetup ensures daemon is running when autoStartDaemon is true, resolves auth, and creates the API client.
func preRunSetup(ctx context.Context, cmd *cobra.Command, baseURL, token string, autoStartDaemon bool) (*client.Client, error) {
	dm := cliOptions.DaemonManager
	if dm == nil {
		dm = daemon.NewDaemonManager(nil)
	}

	if autoStartDaemon {
		if !utils.IsDockerComposeAvailable() {
			fmt.Println("Docker compose is not available. Please install docker compose and try again.")
			fmt.Println("See https://docs.docker.com/compose/install/ for installation instructions.")
			fmt.Println("agent registry uses docker compose to start the server and the agent gateway.")
			return nil, fmt.Errorf("docker compose is not available")
		}
		if !dm.IsRunning() {
			if err := dm.Start(); err != nil {
				return nil, fmt.Errorf("failed to start daemon: %w", err)
			}
		}
	}

	// Get authentication token if no token override was provided
	if token == "" && cliOptions.AuthnProviderFactory != nil {
		resolvedToken, err := resolveAuthToken(ctx, cmd, cliOptions.AuthnProviderFactory)
		if err != nil {
			return nil, err
		}

		token = resolvedToken
	}

	if cliOptions.OnTokenResolved != nil {
		if err := cliOptions.OnTokenResolved(token); err != nil {
			return nil, fmt.Errorf("failed to call resolve token callback: %w", err)
		}
	}

	var c *client.Client
	var err error
	if cliOptions.ClientFactory != nil {
		c, err = cliOptions.ClientFactory(ctx, baseURL, token)
	} else {
		c, err = client.NewClientWithConfig(baseURL, token)
	}
	if err != nil {
		return nil, fmt.Errorf("API client not initialized: %w", err)
	}
	return c, nil
}
