package agent

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/tui"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

var ChatCmd = &cobra.Command{
	Use:   "chat <deployed-agent-name>",
	Short: "Chat with a deployed agent",
	Long: `Chat with a deployed agent through the agent gateway. If --version is not provided,
the command will attempt to find the deployed version automatically. If multiple versions
are deployed, you must specify --version.`,
	Args: cobra.ExactArgs(1),
	RunE: runChat,
	Example: `arctl agent chat my-agent
  arctl agent chat my-agent --version 1.2.3
  arctl agent chat my-agent --gateway-url http://localhost:21212`,
}

func runChat(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	agentName := args[0]
	version, _ := cmd.Flags().GetString("version")
	gatewayURL, _ := cmd.Flags().GetString("gateway-url")

	if envURL := os.Getenv("REGISTRY_URL"); envURL != "" {
		gatewayURL = envURL
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Resolve version if not provided
	if version == "" {
		resolvedVersion, err := resolveDeployedVersion(agentName)
		if err != nil {
			return err
		}
		version = resolvedVersion
	}

	deployment, err := apiClient.GetDeployedServerByNameAndVersion(agentName, version)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}
	if deployment == nil {
		return fmt.Errorf("agent %q version %q is not deployed", agentName, version)
	}

	// Validate it's an agent, not an MCP server
	if deployment.ResourceType != "agent" {
		return fmt.Errorf("%q is not an agent (resource type: %s)", agentName, deployment.ResourceType)
	}

	// Construct agent gateway URL
	agentURL := fmt.Sprintf("%s/agents/%s", gatewayURL, agentName)

	fmt.Printf("Connecting to agent '%s' (version %s) at %s\n", agentName, version, agentURL)

	return launchDeployedChat(cmd.Context(), agentName, agentURL)
}

func resolveDeployedVersion(agentName string) (string, error) {
	deployments, err := apiClient.GetDeployedServers()
	if err != nil {
		return "", fmt.Errorf("failed to get deployments: %w", err)
	}

	var matchingDeployments []*client.DeploymentResponse
	for _, dep := range deployments {
		if dep.ServerName == agentName && dep.ResourceType == "agent" {
			matchingDeployments = append(matchingDeployments, dep)
		}
	}

	if len(matchingDeployments) == 0 {
		return "", fmt.Errorf("no deployed version found for agent %q. Please deploy the agent first or specify --version", agentName)
	}

	if len(matchingDeployments) == 1 {
		return matchingDeployments[0].Version, nil
	}

	// Multiple deployments found
	versions := make([]string, len(matchingDeployments))
	for i, dep := range matchingDeployments {
		versions[i] = dep.Version
	}
	return "", fmt.Errorf("multiple deployed versions found for agent %q: %v. Please specify --version", agentName, versions)
}

func launchDeployedChat(ctx context.Context, agentName string, agentURL string) error {
	sessionID := protocol.GenerateContextID()
	client, err := a2aclient.NewA2AClient(agentURL, a2aclient.WithTimeout(60*time.Second))
	if err != nil {
		return fmt.Errorf("failed to create chat client: %w", err)
	}

	sendFn := func(ctx context.Context, params protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
		ch, err := client.StreamMessage(ctx, params)
		if err != nil {
			return nil, err
		}
		return ch, nil
	}

	return tui.RunChat(agentName, sessionID, sendFn, verbose)
}

func init() {
	ChatCmd.Flags().String("version", "", "Agent version to chat with (if not provided, uses the deployed version)")
	ChatCmd.Flags().String("gateway-url", "http://localhost:21212", "Gateway URL (default: http://localhost:21212, can be overridden via REGISTRY_URL env var)")
}
