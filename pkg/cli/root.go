package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
	"github.com/agentregistry-dev/agentregistry/internal/cli/configure"
	clidaemon "github.com/agentregistry-dev/agentregistry/internal/cli/daemon"
	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/cli/deployment"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp"
	"github.com/agentregistry-dev/agentregistry/internal/cli/prompt"
	"github.com/agentregistry-dev/agentregistry/internal/cli/skill"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/daemon/dockercompose"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/spf13/cobra"
)

// ClientFactory creates an API client for the given base URL and token.
// Used for testing when nil; production uses client.NewClientWithConfig.
type ClientFactory func(ctx context.Context, baseURL, token string) (*client.Client, error)

// CLIOptions configures the CLI behavior.
// Can be extended for more options (e.g. client factory).
type CLIOptions struct {
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
)

// Configure applies options to the root command (e.g. for tests or alternate entry points).
func Configure(opts CLIOptions) {
	cliOptions = opts
}

// Root returns the root cobra command. Used by main and tests.
func Root() *cobra.Command {
	return rootCmd
}

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "Agent Registry CLI",
	Long:  `arctl is a CLI tool for managing agents, MCP servers and skills.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		baseURL, token := resolveRegistryTarget(os.Getenv)
		if preRunBehavior(cmd) {
			return nil
		}

		c, err := preRunSetup(cmd.Context(), cmd, baseURL, token)
		if err != nil {
			return err
		}

		agentutils.SetDefaultRegistryURL(c.BaseURL)
		mcp.SetAPIClient(c)
		agent.SetAPIClient(c)
		skill.SetAPIClient(c)
		prompt.SetAPIClient(c)
		deployment.SetAPIClient(c)
		cli.SetAPIClient(c)
		declarative.SetAPIClient(c)
		return nil
	},
}

func init() {
	rootCmd.PersistentFlags().StringVar(&registryURL, "registry-url", os.Getenv("ARCTL_API_BASE_URL"), "Registry URL (overrides ARCTL_API_BASE_URL env var; defaults to http://localhost:12121)")
	// Don't use the default value from the env var here as the CLI help text would print it and this is a sensitive credential to access the API
	rootCmd.PersistentFlags().StringVar(&registryToken, "registry-token", "", "Registry bearer token (defaults to value of ARCTL_API_TOKEN env var)")

	rootCmd.AddCommand(mcp.McpCmd)
	rootCmd.AddCommand(agent.AgentCmd)
	rootCmd.AddCommand(skill.SkillCmd)
	rootCmd.AddCommand(prompt.PromptCmd)
	rootCmd.AddCommand(configure.ConfigureCmd)
	rootCmd.AddCommand(cli.VersionCmd)
	rootCmd.AddCommand(cli.ImportCmd)
	rootCmd.AddCommand(cli.ExportCmd)
	rootCmd.AddCommand(cli.EmbeddingsCmd)
	rootCmd.AddCommand(deployment.DeploymentCmd)
	rootCmd.AddCommand(clidaemon.New(dockercompose.NewManager(dockercompose.DefaultConfig())))
	rootCmd.AddCommand(declarative.ApplyCmd)
	rootCmd.AddCommand(declarative.GetCmd)
	rootCmd.AddCommand(declarative.DeleteCmd)
	rootCmd.AddCommand(declarative.InitCmd)
	rootCmd.AddCommand(declarative.BuildCmd)
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

// preRunSkipCommands defines which commands skip pre-run setup (no API client needed).
// Key: parent name; value: set of subcommand names that skip setup.
var preRunSkipCommands = map[string]map[string]bool{
	"arctl": {
		"completion": true,
		"configure":  true,
		"version":    true,
		"init":       true,
		"build":      true,
	},
	"agent": {
		"build": true,
		"init":  true,
	},
	"mcp": {
		"add-tool": true,
		"build":    true,
		"init":     true,
	},
	"skill": {
		"build": true,
		"init":  true,
	},
}

// preRunBehavior returns whether to skip pre-run setup (e.g. agent/mcp/skill init).
func preRunBehavior(cmd *cobra.Command) (skipSetup bool) {
	if cmd == nil {
		return false
	}
	for c := cmd; c != nil; c = c.Parent() {
		parent := c.Parent()
		if parent == nil {
			break
		}
		if subcommands, ok := preRunSkipCommands[parent.Name()]; ok && subcommands[c.Name()] {
			return true
		}
	}
	return false
}

// preRunSetup resolves auth and creates the API client.
func preRunSetup(ctx context.Context, cmd *cobra.Command, baseURL, token string) (*client.Client, error) {
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

	factory := cliOptions.ClientFactory
	if factory == nil {
		factory = func(_ context.Context, u, tok string) (*client.Client, error) {
			return client.NewClientWithConfig(u, tok)
		}
	}
	c, err := factory(ctx, baseURL, token)
	if err != nil {
		return nil, fmt.Errorf("registry unreachable at %s: %w", baseURL, err)
	}
	return c, nil
}
