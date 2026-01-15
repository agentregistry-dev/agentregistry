package mcp

import (
	"fmt"
	"strings"

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
	DeployCmd.Flags().StringVar(&deployVersion, "version", "latest", "Version to deploy")
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

	if deployVersion == "" {
		return fmt.Errorf("version is required")
	}

	// Ensure the server with the specified version is published and approved
	server, err := apiClient.GetServerByNameAndVersion(serverName, deployVersion, true, true)
	if err != nil {
		return fmt.Errorf("failed to get server: %w", err)
	}
	if server == nil {
		return fmt.Errorf("server not found: %s", serverName)
	}

	isPublished, err := isServerPublished(serverName, deployVersion)
	if err != nil {
		return fmt.Errorf("failed to check if server is published: %w", err)
	}
	if !isPublished {
		return fmt.Errorf("server %s version %s is not published", serverName, deployVersion)
	}

	// Ensure the server is approved
	isApproved, err := isServerApproved(serverName, deployVersion)
	if err != nil {
		return fmt.Errorf("failed to check if server is approved: %w", err)
	}
	if !isApproved {
		return fmt.Errorf("server %s version %s is not approved for deployment", serverName, deployVersion)
	}

	// Deploy server via API (server will handle reconciliation)
	fmt.Println("\nDeploying server...")
	deployment, err := apiClient.DeployServer(server.Server.Name, deployVersion, config, deployPreferRemote)
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
