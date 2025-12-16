package agent

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/models"
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

func runPublish(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &config.Config{}
	publishCfg := &publishAgentCfg{
		Config: cfg,
	}
	publishCfg.Version = publishVersion
	publishCfg.GitHubRepository = githubRepository

	arg := args[0]

	// If --version flag was provided, treat as registry-based publish
	// No need to push the agent, just mark as published
	if publishCfg.Version != "" {
		agentName := arg
		version := publishCfg.Version

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
		publishCfg.ProjectDir = arg
		publishCfg.Version = "latest"
		return publishAgent(publishCfg)
	}
	return nil
}

type publishAgentCfg struct {
	Config           *config.Config
	ProjectDir       string
	Version          string
	GitHubRepository string
}

func publishAgent(cfg *publishAgentCfg) error {
	// Validate project directory
	if cfg.ProjectDir == "" {
		return fmt.Errorf("project directory is required")
	}

	// Check if project directory exists
	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	version := "latest"
	if cfg.Version != "" {
		version = cfg.Version
	}

	mgr := common.NewManifestManager(cfg.ProjectDir)
	manifest, err := mgr.Load()
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	jsn := &models.AgentJSON{
		AgentManifest: *manifest,
		Version:       version,
		Status:        "active",
	}

	if cfg.GitHubRepository != "" {
		jsn.Repository = &model.Repository{
			URL:    cfg.GitHubRepository,
			Source: "github",
		}
	}

	_, err = apiClient.PublishAgent(jsn)
	if err != nil {
		return fmt.Errorf("failed to publish agent: %w", err)
	}

	fmt.Printf("Agent '%s' version %s published successfully\n", jsn.Name, jsn.Version)

	return nil
}
