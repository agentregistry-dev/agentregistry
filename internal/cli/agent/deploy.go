package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/spf13/cobra"
)

var DeployCmd = &cobra.Command{
	Use:   "deploy [agent-name]",
	Short: "Deploy an agent",
	Long: `Deploy an agent from the registry.

Example:
  arctl agent deploy my-agent --version latest
  arctl agent deploy my-agent --version 1.2.3
  arctl agent deploy my-agent --version latest --provider-id kubernetes-default`,
	Args: cobra.ExactArgs(1),
	RunE: runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	name := args[0]
	version, _ := cmd.Flags().GetString("version")
	providerID, _ := cmd.Flags().GetString("provider-id")
	namespace, _ := cmd.Flags().GetString("namespace")

	if version == "" {
		version = "latest"
	}

	if providerID == "" {
		providerID = "local"
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	agentModel, err := apiClient.GetAgentByNameAndVersion(name, version)
	if err != nil {
		return fmt.Errorf("failed to fetch agent %q: %w", name, err)
	}
	if agentModel == nil {
		return fmt.Errorf("agent not found: %s (version %s)", name, version)
	}

	manifest := &agentModel.Agent.AgentManifest

	// Validate that required API keys are set
	if err := validateAPIKey(manifest.ModelProvider); err != nil {
		return err
	}

	// Build config map with environment variables
	// TODO: need to figure out how do we
	// store/configure MCP servers agents is referencing.
	// They are part of the agent.yaml, so we should store them
	// in the config, then when doing reconciliation, we can deploy them as well.
	config := buildDeployConfig(manifest)
	if namespace != "" {
		config["KAGENT_NAMESPACE"] = namespace
	}

	if providerID == "local" {
		return deployLocal(name, version, config, providerID)
	}
	return deployToProvider(name, version, config, namespace, providerID)
}

// buildDeployConfig creates the configuration map with all necessary environment variables
func buildDeployConfig(manifest *models.AgentManifest) map[string]string {
	config := make(map[string]string)

	// Add model provider API key if available
	providerAPIKeys := map[string]string{
		"openai":      "OPENAI_API_KEY",
		"anthropic":   "ANTHROPIC_API_KEY",
		"azureopenai": "AZUREOPENAI_API_KEY",
		"gemini":      "GOOGLE_API_KEY",
	}

	if envVar, ok := providerAPIKeys[strings.ToLower(manifest.ModelProvider)]; ok && envVar != "" {
		if value := os.Getenv(envVar); value != "" {
			config[envVar] = value
		}
	}

	if manifest.TelemetryEndpoint != "" {
		config["OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"] = manifest.TelemetryEndpoint
	}

	return config
}

// deployLocal deploys an agent to the local provider
func deployLocal(name, version string, config map[string]string, providerID string) error {
	deployment, err := apiClient.DeployAgent(name, version, config, providerID)
	if err != nil {
		return fmt.Errorf("failed to deploy agent: %w", err)
	}

	fmt.Printf("Agent '%s' version '%s' deployed to local provider (providerId=%s)\n", deployment.ServerName, deployment.Version, providerID)
	return nil
}

// deployToProvider deploys an agent to a non-local provider.
func deployToProvider(name, version string, config map[string]string, namespace string, providerID string) error {
	deployment, err := apiClient.DeployAgent(name, version, config, providerID)
	if err != nil {
		return fmt.Errorf("failed to deploy agent: %w", err)
	}

	ns := namespace
	if ns == "" {
		ns = "(default)"
	}
	fmt.Printf("Agent '%s' version '%s' deployed to providerId=%s in namespace '%s'\n", deployment.ServerName, deployment.Version, providerID, ns)
	return nil
}

func init() {
	DeployCmd.Flags().String("version", "latest", "Agent version to deploy")
	DeployCmd.Flags().String("provider-id", "", "Deployment target provider ID (defaults to local when omitted)")
	DeployCmd.Flags().Bool("prefer-remote", false, "Prefer using a remote source when available")
	DeployCmd.Flags().String("namespace", "", "Kubernetes namespace for agent deployment (defaults to current kubeconfig context)")
}
