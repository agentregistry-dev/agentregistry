package mcp

import (
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/spf13/cobra"
)

var (
	deployVersion      string
	deployEnv          []string
	deployArgs         []string
	deployHeaders      []string
	deployPreferRemote bool
	deployYes          bool
	deployProviderID   string
	deployNamespace    string
	deployWait         bool
)

var DeployCmd = &cobra.Command{
	Use:           "deploy <server-name>",
	Short:         "Deploy an MCP server",
	Long:          `Deploy an MCP server to a provider.`,
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
	DeployCmd.Flags().StringVar(&deployProviderID, "provider-id", "", "Deployment target provider ID (defaults to local when omitted)")
	DeployCmd.Flags().StringVar(&deployNamespace, "namespace", "", "Kubernetes namespace for deployment (if provider targets Kubernetes)")
	DeployCmd.Flags().BoolVar(&deployWait, "wait", true, "Wait for the deployment to become ready before returning")
}

func runDeploy(cmd *cobra.Command, args []string) error {
	serverName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	deploymentEnv := make(map[string]string)

	if deployProviderID == "" {
		deployProviderID = "local"
	}

	for _, env := range deployEnv {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid env format (expected KEY=VALUE): %s", env)
		}
		deploymentEnv[parts[0]] = parts[1]
	}

	for _, arg := range deployArgs {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid arg format (expected KEY=VALUE): %s", arg)
		}
		deploymentEnv["ARG_"+parts[0]] = parts[1]
	}

	for _, header := range deployHeaders {
		parts := strings.SplitN(header, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header format (expected KEY=VALUE): %s", header)
		}
		deploymentEnv["HEADER_"+parts[0]] = parts[1]
	}

	// Add namespace to deployment env for Kubernetes deployments
	if deployNamespace != "" {
		deploymentEnv["KAGENT_NAMESPACE"] = deployNamespace
	}

	if deployVersion == "" {
		return fmt.Errorf("version is required")
	}

	// Deploy server via API (server will handle reconciliation)
	fmt.Println("\nDeploying server...")
	deployment, err := apiClient.DeployServer(serverName, deployVersion, deploymentEnv, deployPreferRemote, deployProviderID)
	if err != nil {
		return fmt.Errorf("failed to deploy server: %w", err)
	}

	if deployProviderID != "local" && deployWait {
		fmt.Printf("Waiting for server '%s' to become ready...\n", deployment.ServerName)
		if err := common.WaitForDeploymentReady(apiClient, deployment.ID); err != nil {
			return err
		}
	}

	fmt.Printf("\n✓ Deployed %s (%s) with providerId=%s\n", deployment.ServerName, common.FormatVersionForDisplay(deployment.Version), deployProviderID)
	if deployNamespace != "" {
		ns := deployNamespace
		fmt.Printf("Namespace: %s\n", ns)
	}
	if len(deploymentEnv) > 0 {
		fmt.Printf("Deployment Env: %d setting(s)\n", len(deploymentEnv))
	}
	if deployProviderID == "local" {
		fmt.Printf("\nServer deployment recorded. The registry will reconcile containers automatically.\n")
		fmt.Printf("Agent Gateway endpoint: http://localhost:%s/mcp\n", common.DefaultAgentGatewayPort)
	}

	return nil
}
