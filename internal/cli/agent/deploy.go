package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/spf13/cobra"
)

var DeployCmd = &cobra.Command{
	Use:   "deploy [agent-name]",
	Short: "Deploy an agent",
	Long: `Deploy an agent from the registry.

Example:
  arctl agent deploy my-agent --version latest
  arctl agent deploy my-agent --version 1.2.3
  arctl agent deploy my-agent --version latest --runtime kubernetes`,
	Args: cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		runtimeFlag, _ := cmd.Flags().GetString("runtime")
		if runtimeFlag != "" {
			if err := runtime.ValidateRuntime(runtimeFlag); err != nil {
				return err
			}
		}
		return nil
	},
	RunE: runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	name := args[0]
	version, _ := cmd.Flags().GetString("version")
	runtime, _ := cmd.Flags().GetString("runtime")

	if version == "" {
		version = "latest"
	}

	if runtime == "" {
		runtime = "local"
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Get the latest published and approved version of the agent
	agentModel, err := apiClient.GetAgentByNameAndVersion(name, version, true, true)
	if err != nil {
		return fmt.Errorf("failed to fetch agent %q: %w", name, err)
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

	// Handle runtime-specific deployment logic
	switch runtime {
	case "local":
		return deployLocal(name, version, config)
	case "kubernetes":
		return deployKubernetes(name, version, config)
	default:
		// This shouldn't happen due to PreRunE validation, but handle gracefully
		return fmt.Errorf("unimplemented runtime: %s", runtime)
	}
}

// buildDeployConfig creates the configuration map with all necessary environment variables
func buildDeployConfig(manifest *common.AgentManifest) map[string]string {
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

// deployLocal deploys an agent to the local/docker runtime
func deployLocal(name, version string, config map[string]string) error {
	deployment, err := apiClient.DeployAgent(name, version, config)
	if err != nil {
		return fmt.Errorf("failed to deploy agent: %w", err)
	}

	fmt.Printf("Agent '%s' version '%s' deployed to local runtime\n", deployment.ServerName, deployment.Version)
	return nil
}

// deployKubernetes deploys an agent to the kubernetes runtime
func deployKubernetes(name, version string, config map[string]string) error {
	// TODO: Implement kubernetes deployment logic
	// This would involve:
	// 1. Creating kubernetes manifests (Deployment, Service, ConfigMap, etc.)
	// 2. Applying them to the cluster
	// 3. Waiting for the deployment to be ready
	return fmt.Errorf("kubernetes runtime deployment is not yet implemented")
}

func init() {
	DeployCmd.Flags().String("version", "latest", "Agent version to deploy")
	DeployCmd.Flags().String("runtime", "local", "Deployment runtime target (local, kubernetes)")
	DeployCmd.Flags().Bool("prefer-remote", false, "Prefer using a remote source when available")
}
