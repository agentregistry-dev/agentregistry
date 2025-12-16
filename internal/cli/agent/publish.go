package agent

import (
	"fmt"
	"os"

	arConfig "github.com/agentregistry-dev/agentregistry/pkg/cli/config"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
)

var PublishCmd = &cobra.Command{
	Use:   "publish [project-directory|agent-name]",
	Short: "Publish an agent project to the registry",
	Long: `Publish an agent project to the registry.

This command supports two forms:

- 'arctl agent publish ./my-agent' publishes the agent defined by agent.yaml in the given folder.
- 'arctl agent publish my-agent --version 1.2.3' publishes an agent that already exists in the registry by name and version.

Examples:
arctl agent publish ./my-agent
arctl agent publish my-agent --version latest`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPublish,
	Example: `arctl agent publish ./my-agent`,
}

var publishVersion string
var githubRepository string

func init() {
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Specify version to publish (when publishing an existing registry agent)")
	PublishCmd.Flags().StringVar(&githubRepository, "github", "", "Specify the GitHub repository for the agent")
}

type publishAgentCfg struct {
	AgentCfg         *agentCfg
	GitHubRepository string
}

func runPublish(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &config.Config{}
	publishCfg := &publishAgentCfg{
		AgentCfg: &agentCfg{
			Config:  cfg,
			Version: publishVersion,
		},
		GitHubRepository: githubRepository,
	}

	arg := args[0]

	// If --version flag was provided, treat as registry-based publish
	// No need to push the agent, just mark as published
	if publishCfg.AgentCfg.Version != "" {
		agentName := arg
		version := publishCfg.AgentCfg.Version

		if apiClient == nil {
			return fmt.Errorf("API client not initialized")
		}

		if err := apiClient.PublishAgentStatus(agentName, version); err != nil {
			return fmt.Errorf("failed to publish agent: %w", err)
		}

		fmt.Printf("Agent '%s' version %s published successfully\n", agentName, version)

		return nil
	}

	// If the argument is a directory containing an agent project, publish from local
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		publishCfg.AgentCfg.ProjectDir = arg
		publishCfg.AgentCfg.Version = "latest"
		jsn, err := createAgentJSONFromCfg(publishCfg.AgentCfg)
		if err != nil {
			return fmt.Errorf("failed to create agent JSON: %w", err)
		}

		if publishCfg.GitHubRepository != "" {
			jsn.Repository = &model.Repository{
				URL:    publishCfg.GitHubRepository,
				Source: "github",
			}
		}

		// Push the agent (creates unpublished entry)
		if _, err := apiClient.PushAgent(jsn); err != nil {
			return fmt.Errorf("failed to push agent: %w", err)
		}

		// Auto-approve the agent (if configured)
		if arConfig.GetAutoApprove() {
			if err := apiClient.ApproveAgentStatus(jsn.Name, jsn.Version, "Auto-approved via publish command"); err != nil {
				return fmt.Errorf("failed to approve agent: %w", err)
			}
		}

		// Mark the agent as published
		if err := apiClient.PublishAgentStatus(jsn.Name, jsn.Version); err != nil {
			return fmt.Errorf("failed to publish agent: %w", err)
		}

		return nil
	}
	return fmt.Errorf("invalid argument: %s must be a directory", arg)
}
