package agent

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
)

var PublishCmd = &cobra.Command{
	Use:   "publish [project-directory]",
	Short: "Publish an agent project to the registry",
	Long: `Publish an agent project to the registry.

The project directory must contain an agent.yaml manifest.

Examples:
arctl agent publish ./my-agent`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPublish,
	Example: `arctl agent publish ./my-agent`,
}

var publishVersion string
var githubRepository string

func init() {
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Override the version from the manifest")
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

	// If the argument is a directory containing an agent project, publish from local
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		publishCfg.ProjectDir = arg
		return publishAgent(publishCfg)
	}
	return fmt.Errorf("argument must be a directory containing an agent project")
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

	_, err = apiClient.CreateAgent(jsn)
	if err != nil {
		return fmt.Errorf("failed to publish agent: %w", err)
	}

	fmt.Printf("Agent '%s' version %s published successfully\n", jsn.Name, jsn.Version)

	return nil
}
