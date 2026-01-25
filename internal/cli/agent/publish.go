package agent

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

var PublishCmd = &cobra.Command{
	Use:   "publish [project-directory|agent-name]",
	Short: "Publish an agent project to the registry",
	Long: `Publish an agent project to the registry.

This command supports three forms:

- 'arctl agent publish ./my-agent' publishes the agent defined by agent.yaml in the given folder.
- 'arctl agent publish my-agent --version 1.2.3' publishes an agent that already exists in the registry by name and version.
- 'arctl agent publish --from-github https://github.com/org/repo' publishes an agent directly from a GitHub repository.

Examples:
arctl agent publish ./my-agent
arctl agent publish my-agent --version latest
arctl agent publish --from-github https://github.com/myorg/my-agent
arctl agent publish --from-github https://github.com/myorg/my-agent --branch develop`,
	Args:    cobra.MaximumNArgs(1),
	RunE:    runPublish,
	Example: `arctl agent publish ./my-agent`,
}

var (
	publishVersion   string
	githubRepository string
	fromGitHub       string
	gitBranch        string
)

func init() {
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Specify version to publish (when publishing an existing registry agent)")
	PublishCmd.Flags().StringVar(&githubRepository, "github", "", "Specify the GitHub repository for the agent")
	PublishCmd.Flags().StringVar(&fromGitHub, "from-github", "", "Publish agent directly from a GitHub repository URL")
	PublishCmd.Flags().StringVar(&gitBranch, "branch", "main", "Branch to use when publishing from GitHub")
}

func runPublish(cmd *cobra.Command, args []string) error {
	if fromGitHub != "" {
		return publishAgentFromGitHub(fromGitHub, gitBranch, publishVersion)
	}

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

	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		publishCfg.ProjectDir = arg
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

	mgr := common.NewManifestManager(cfg.ProjectDir)
	manifest, err := mgr.Load()
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	// Determine version: flag > manifest > default
	version := "latest"
	if cfg.Version != "" {
		version = cfg.Version
	} else if manifest.Version != "" {
		version = manifest.Version
	}

	// Create a copy of the manifest without telemetryEndpoint for registry publishing
	// since telemetry is a deployment/runtime concern, not stored in the registry
	publishManifest := *manifest
	publishManifest.TelemetryEndpoint = ""

	jsn := &models.AgentJSON{
		AgentManifest: publishManifest,
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

func publishAgentFromGitHub(repoURL, branch, version string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	repoInfo, err := utils.ParseGitHubURL(repoURL)
	if err != nil {
		return fmt.Errorf("invalid GitHub URL: %w", err)
	}

	if branch != "" {
		repoInfo.Branch = branch
	}

	fmt.Printf("Fetching agent.yaml from %s (branch: %s)...\n", repoInfo.GetGitHubRepoURL(), repoInfo.Branch)

	content, err := utils.FetchGitHubRawFile(repoInfo, "agent.yaml")
	if err != nil {
		return fmt.Errorf("failed to fetch agent.yaml: %w", err)
	}

	var manifest common.AgentManifest
	if err := yaml.Unmarshal(content, &manifest); err != nil {
		return fmt.Errorf("failed to parse agent.yaml: %w", err)
	}

	if version == "" {
		if manifest.Version != "" {
			version = manifest.Version
		} else {
			version = "latest"
		}
	}

	manifest.TelemetryEndpoint = ""

	jsn := &models.AgentJSON{
		AgentManifest: manifest,
		Version:       version,
		Status:        "active",
		Repository: &model.Repository{
			URL:    repoInfo.GetGitHubRepoURL(),
			Source: "github",
		},
	}

	_, err = apiClient.PublishAgent(jsn)
	if err != nil {
		return fmt.Errorf("failed to publish agent: %w", err)
	}

	fmt.Printf("Agent '%s' version %s published successfully from GitHub: %s\n", jsn.Name, jsn.Version, repoInfo.GetGitHubRepoURL())

	return nil
}
