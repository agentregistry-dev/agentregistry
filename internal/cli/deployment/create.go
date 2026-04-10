package deployment

import (
	"fmt"
	"maps"
	"os"
	"strings"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	cliUtils "github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/spf13/cobra"
)

var CreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a deployment",
	Long: `Create a deployment for an agent or MCP server from the registry.

Example:
  arctl deployments create my-agent --type agent --version latest
  arctl deployments create my-mcp-server --type mcp --version 1.2.3
  arctl deployments create my-agent --type agent --provider-id kubernetes-default`,
	Args:          cobra.ExactArgs(1),
	RunE:          runCreate,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	CreateCmd.Flags().String("type", "", "Resource type to deploy (agent or mcp)")
	CreateCmd.Flags().String("version", "latest", "Version to deploy")
	CreateCmd.Flags().String("provider-id", "", "Deployment target provider ID (defaults to local when omitted)")
	CreateCmd.Flags().String("namespace", "", "Kubernetes namespace for deployment")
	CreateCmd.Flags().Bool("wait", true, "Wait for the deployment to become ready before returning")
	CreateCmd.Flags().Bool("prefer-remote", false, "Prefer using a remote source when available")
	CreateCmd.Flags().StringArrayP("env", "e", []string{}, "Environment variables to set (KEY=VALUE)")
	CreateCmd.Flags().StringArrayP("arg", "a", []string{}, "Runtime arguments for MCP servers (KEY=VALUE)")
	CreateCmd.Flags().StringArray("header", []string{}, "HTTP headers for remote MCP servers (KEY=VALUE)")

	_ = CreateCmd.MarkFlagRequired("type")
}

// providerAPIKeys maps model providers to their expected API key env var names.
var providerAPIKeys = map[string]string{
	"openai":      "OPENAI_API_KEY",
	"anthropic":   "ANTHROPIC_API_KEY",
	"azureopenai": "AZUREOPENAI_API_KEY",
	"gemini":      "GOOGLE_API_KEY",
}

func runCreate(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	name := args[0]
	resourceType, _ := cmd.Flags().GetString("type")
	version, _ := cmd.Flags().GetString("version")
	providerID, _ := cmd.Flags().GetString("provider-id")
	namespace, _ := cmd.Flags().GetString("namespace")
	wait, _ := cmd.Flags().GetBool("wait")
	preferRemote, _ := cmd.Flags().GetBool("prefer-remote")
	envFlags, _ := cmd.Flags().GetStringArray("env")
	argFlags, _ := cmd.Flags().GetStringArray("arg")
	headerFlags, _ := cmd.Flags().GetStringArray("header")

	resourceType = strings.ToLower(resourceType)
	if resourceType != "agent" && resourceType != "mcp" {
		return fmt.Errorf("invalid --type %q: must be 'agent' or 'mcp'", resourceType)
	}

	if version == "" {
		version = "latest"
	}
	if providerID == "" {
		providerID = "local"
	}

	envMap, err := cliUtils.ParseEnvFlags(envFlags)
	if err != nil {
		return err
	}

	// Parse --arg flags (MCP-specific, prefixed with ARG_)
	for _, arg := range argFlags {
		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid arg format (expected KEY=VALUE): %s", arg)
		}
		envMap["ARG_"+parts[0]] = parts[1]
	}

	// Parse --header flags (MCP-specific, prefixed with HEADER_)
	for _, header := range headerFlags {
		parts := strings.SplitN(header, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid header format (expected KEY=VALUE): %s", header)
		}
		envMap["HEADER_"+parts[0]] = parts[1]
	}

	if namespace != "" {
		envMap["KAGENT_NAMESPACE"] = namespace
	}

	switch resourceType {
	case "agent":
		return createAgentDeployment(name, version, envMap, providerID, namespace, wait)
	case "mcp":
		return createMCPDeployment(name, version, envMap, providerID, namespace, preferRemote, wait)
	}
	return nil
}

func createAgentDeployment(name, version string, envMap map[string]string, providerID, namespace string, wait bool) error {
	agentModel, err := apiClient.GetAgentVersion(name, version)
	if err != nil {
		return fmt.Errorf("failed to fetch agent %q: %w", name, err)
	}
	if agentModel == nil {
		return fmt.Errorf("agent not found: %s (version %s)", name, version)
	}

	manifest := &agentModel.Agent.AgentManifest

	if err := validateAPIKey(manifest.ModelProvider, envMap); err != nil {
		return err
	}

	config := buildAgentDeployConfig(manifest, envMap)
	if namespace != "" {
		config["KAGENT_NAMESPACE"] = namespace
	}

	if providerID == "local" {
		deployment, err := apiClient.DeployAgent(name, version, config, providerID)
		if err != nil {
			return fmt.Errorf("failed to deploy agent: %w", err)
		}
		fmt.Printf("Agent '%s' version '%s' deployed to local provider (providerId=%s)\n", deployment.ServerName, deployment.Version, providerID)
		return nil
	}

	deployment, err := apiClient.DeployAgent(name, version, config, providerID)
	if err != nil {
		return fmt.Errorf("failed to deploy agent: %w", err)
	}

	if wait {
		fmt.Printf("Waiting for agent '%s' to become ready...\n", deployment.ServerName)
		if err := cliCommon.WaitForDeploymentReady(apiClient, deployment.ID); err != nil {
			return err
		}
	}

	ns := namespace
	if ns == "" {
		ns = "(default)"
	}
	fmt.Printf("Agent '%s' version '%s' deployed to providerId=%s in namespace '%s'\n", deployment.ServerName, deployment.Version, providerID, ns)
	return nil
}

func createMCPDeployment(name, version string, envMap map[string]string, providerID, namespace string, preferRemote bool, wait bool) error {
	fmt.Println("\nDeploying server...")
	deployment, err := apiClient.DeployServer(name, version, envMap, preferRemote, providerID)
	if err != nil {
		return fmt.Errorf("failed to deploy server: %w", err)
	}

	if providerID != "local" && wait {
		fmt.Printf("Waiting for server '%s' to become ready...\n", deployment.ServerName)
		if err := cliCommon.WaitForDeploymentReady(apiClient, deployment.ID); err != nil {
			return err
		}
	}

	fmt.Printf("\nDeployed %s (%s) with providerId=%s\n", deployment.ServerName, cliCommon.FormatVersionForDisplay(deployment.Version), providerID)
	if namespace != "" {
		fmt.Printf("Namespace: %s\n", namespace)
	}
	if len(envMap) > 0 {
		fmt.Printf("Deployment Env: %d setting(s)\n", len(envMap))
	}
	if providerID == "local" {
		fmt.Printf("\nServer deployment recorded. The registry will reconcile containers automatically.\n")
		fmt.Printf("Agent Gateway endpoint: http://localhost:%s/mcp\n", cliCommon.DefaultAgentGatewayPort)
	}

	return nil
}

// validateAPIKey checks that the required API key for the given model provider is set.
func validateAPIKey(modelProvider string, extraEnv map[string]string) error {
	envVar, ok := providerAPIKeys[strings.ToLower(modelProvider)]
	if !ok || envVar == "" {
		return nil
	}
	if v, exists := extraEnv[envVar]; exists && v != "" {
		return nil
	}
	if os.Getenv(envVar) == "" {
		return fmt.Errorf("required API key %s not set for model provider %s", envVar, modelProvider)
	}
	return nil
}

// buildAgentDeployConfig creates the configuration map with all necessary environment variables.
func buildAgentDeployConfig(manifest *models.AgentManifest, envOverrides map[string]string) map[string]string {
	config := make(map[string]string)
	maps.Copy(config, envOverrides)

	if envVar, ok := providerAPIKeys[strings.ToLower(manifest.ModelProvider)]; ok && envVar != "" {
		if _, exists := config[envVar]; !exists {
			if value := os.Getenv(envVar); value != "" {
				config[envVar] = value
			}
		}
	}

	if manifest.TelemetryEndpoint != "" {
		config["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] = manifest.TelemetryEndpoint
	}

	return config
}
