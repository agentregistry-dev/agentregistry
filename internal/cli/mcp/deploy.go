package mcp

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/spf13/cobra"
)

var (
	deployVersion      string
	deployEnv          []string
	deployArgs         []string
	deployHeaders      []string
	deployPreferRemote bool
	deployYes          bool
)

var DeployCmd = &cobra.Command{
	Use:           "deploy <server-name>",
	Short:         "Deploy an MCP server",
	Long:          `Deploy an MCP server to the runtime.`,
	Args:          cobra.ExactArgs(1),
	RunE:          runDeploy,
	SilenceUsage:  true,  // Don't show usage on deployment errors
	SilenceErrors: false, // Still show error messages
}

func init() {
	DeployCmd.Flags().StringVarP(&deployVersion, "version", "v", "latest", "Version to deploy")
	DeployCmd.Flags().StringArrayVarP(&deployEnv, "env", "e", []string{}, "Environment variables (KEY=VALUE)")
	DeployCmd.Flags().StringArrayVarP(&deployArgs, "arg", "a", []string{}, "Runtime arguments (KEY=VALUE)")
	DeployCmd.Flags().StringArrayVar(&deployHeaders, "header", []string{}, "HTTP headers for remote servers (KEY=VALUE)")
	DeployCmd.Flags().BoolVar(&deployPreferRemote, "prefer-remote", false, "Prefer remote deployment over local")
	DeployCmd.Flags().BoolVarP(&deployYes, "yes", "y", false, "Automatically accept all prompts (use default/latest version)")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	serverName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	config := make(map[string]string)

	for _, env := range deployEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid env format (expected KEY=VALUE): %s", env)
		}
		config[parts[0]] = parts[1]
	}

	for _, arg := range deployArgs {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid arg format (expected KEY=VALUE): %s", arg)
		}
		config["ARG_"+parts[0]] = parts[1]
	}

	for _, header := range deployHeaders {
		parts := strings.SplitN(header, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header format (expected KEY=VALUE): %s", header)
		}
		config["HEADER_"+parts[0]] = parts[1]
	}

	server, err := selectServerVersion(serverName, deployVersion, deployYes)
	if err != nil {
		return err
	}

	// Deploy server via API (server will handle reconciliation)
	fmt.Println("\nDeploying server...")
	deployment, err := apiClient.DeployServer(server.Server.Name, server.Server.Version, config, deployPreferRemote)
	if err != nil {
		return fmt.Errorf("failed to deploy server: %w", err)
	}

	fmt.Printf("\n✓ Deployed %s (v%s)\n", deployment.ServerName, deployment.Version)
	if len(config) > 0 {
		fmt.Printf("Configuration: %d setting(s)\n", len(config))
	}
	fmt.Printf("\nServer deployment recorded. The registry will reconcile containers automatically.\n")
	fmt.Printf("Agent Gateway endpoint: http://localhost:21212/mcp\n")

	return nil
}

// selectServerVersion handles server version selection logic with interactive prompts
// Returns the selected server or an error if not found or cancelled
func selectServerVersion(resourceName, requestedVersion string, autoYes bool) (*v0.ServerResponse, error) {
	if apiClient == nil {
		return nil, fmt.Errorf("API client not initialized")
	}

	// If a specific version was requested, try to get that version
	if requestedVersion != "" && requestedVersion != "latest" {
		fmt.Printf("Checking if MCP server '%s' version '%s' exists in registry...\n", resourceName, requestedVersion)
		server, err := apiClient.GetServerByNameAndVersion(resourceName, requestedVersion)
		if err != nil {
			return nil, fmt.Errorf("error querying registry: %w", err)
		}
		if server == nil {
			return nil, fmt.Errorf("MCP server '%s' version '%s' not found in registry", resourceName, requestedVersion)
		}
		fmt.Printf("✓ Found MCP server: %s (version %s)\n", server.Server.Name, server.Server.Version)
		return server, nil
	}

	// No specific version requested, check all versions
	fmt.Printf("Checking if MCP server '%s' exists in registry...\n", resourceName)
	versions, err := apiClient.GetServerVersions(resourceName)
	if err != nil {
		return nil, fmt.Errorf("error querying registry: %w", err)
	}

	if len(versions) == 0 {
		return nil, fmt.Errorf("MCP server '%s' not found in registry. Use 'arctl mcp list' to see available servers", resourceName)
	}

	// Get the latest version (first in the list, as they're ordered by date)
	latestServer := &versions[0]

	// If there are multiple versions, prompt the user (unless --yes is set)
	if len(versions) > 1 {
		fmt.Printf("✓ Found %d versions of MCP server '%s':\n", len(versions), resourceName)
		for i, v := range versions {
			marker := ""
			if i == 0 {
				marker = " (latest)"
			}
			fmt.Printf("  - %s%s\n", v.Server.Version, marker)
		}
		fmt.Printf("\nDefault: version %s (latest)\n", latestServer.Server.Version)

		// Skip prompt if --yes flag is set
		if !autoYes {
			// Prompt user for confirmation
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Proceed with the latest version? [Y/n]: ")
			response, err := reader.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("error reading input: %w", err)
			}

			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				return nil, fmt.Errorf("operation cancelled. To use a specific version, use: --version <version>")
			}
		} else {
			fmt.Println("Auto-accepting latest version (--yes flag set)")
		}
	} else {
		// Only one version available
		fmt.Printf("✓ Found MCP server: %s (version %s)\n", latestServer.Server.Name, latestServer.Server.Version)
	}

	return latestServer, nil
}
