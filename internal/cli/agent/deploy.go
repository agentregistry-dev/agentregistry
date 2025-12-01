package agent

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/spf13/cobra"
)

var DeployCmd = &cobra.Command{
	Use:   "deploy [agent-name]",
	Short: "Deploy an agent",
	Long: `Deploy an agent from the registry.

Example:
  arctl agent deploy my-agent --version latest
  arctl agent deploy my-agent --version 1.2.3`,
	Args: cobra.ExactArgs(1),
	RunE: runDeploy,
}

func runDeploy(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	name := args[0]
	version, _ := cmd.Flags().GetString("version")

	if version == "" {
		version = "latest"
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	agentModel, err := apiClient.GetAgentByNameAndVersion(name, version)
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

	// Deploy the agent with the config
	deployment, err := apiClient.DeployAgent(name, version, config)
	if err != nil {
		return fmt.Errorf("failed to deploy agent: %w", err)
	}

	fmt.Printf("Agent '%s' version '%s' deployed\n", deployment.ServerName, deployment.Version)
	return nil
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

	return config
}

func init() {
	DeployCmd.Flags().String("version", "latest", "Agent version to deploy")
	DeployCmd.Flags().Bool("prefer-remote", false, "Prefer using a remote source when available")
}
