package cli

import (
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/client"
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

var deployCmd = &cobra.Command{
	Use:           "deploy <resource-type> <resource-name>",
	Short:         "Deploy a resource",
	Long:          `Deploy resources (mcp server, agent) to the runtime.`,
	Args:          cobra.ExactArgs(2),
	RunE:          runDeploy,
	SilenceUsage:  true,  // Don't show usage on deployment errors
	SilenceErrors: false, // Still show error messages
}

var removeCmd = &cobra.Command{
	Use:           "remove <resource-type> <resource-name>",
	Short:         "Remove a deployed resource",
	Long:          `Remove a deployed resource from the runtime.`,
	Args:          cobra.ExactArgs(2),
	RunE:          runRemove,
	SilenceUsage:  true,  // Don't show usage on removal errors
	SilenceErrors: false, // Still show error messages
}

func init() {
	deployCmd.Flags().StringVarP(&deployVersion, "version", "v", "latest", "Version to deploy")
	deployCmd.Flags().StringArrayVarP(&deployEnv, "env", "e", []string{}, "Environment variables (KEY=VALUE)")
	deployCmd.Flags().StringArrayVarP(&deployArgs, "arg", "a", []string{}, "Runtime arguments (KEY=VALUE)")
	deployCmd.Flags().StringArrayVar(&deployHeaders, "header", []string{}, "HTTP headers for remote servers (KEY=VALUE)")
	deployCmd.Flags().BoolVar(&deployPreferRemote, "prefer-remote", false, "Prefer remote deployment over local")
	deployCmd.Flags().BoolVarP(&deployYes, "yes", "y", false, "Automatically accept all prompts (use default/latest version)")

	rootCmd.AddCommand(deployCmd)
	rootCmd.AddCommand(removeCmd)
}

func runDeploy(cmd *cobra.Command, args []string) error {
	resourceType := args[0]
	resourceName := args[1]

	if resourceType != "mcp" {
		return fmt.Errorf("only 'mcp' resource type is supported, got: %s", resourceType)
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

	apiClient, err := client.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	server, err := selectServerVersion(resourceName, deployVersion, deployYes)
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

func runRemove(cmd *cobra.Command, args []string) error {
	resourceType := args[0]
	resourceName := args[1]

	// Only MCP servers are supported for now
	if resourceType != "mcp" {
		return fmt.Errorf("only 'mcp' resource type is supported, got: %s", resourceType)
	}

	// Create API client
	apiClient, err := client.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Remove server via API (server will handle reconciliation)
	fmt.Printf("Removing %s from deployments...\n", resourceName)
	err = apiClient.RemoveServer(resourceName)
	if err != nil {
		return fmt.Errorf("failed to remove server: %w", err)
	}

	fmt.Printf("\n✓ Removed %s\n", resourceName)
	fmt.Println("Server removal recorded. The registry will reconcile containers automatically.")

	return nil
}
