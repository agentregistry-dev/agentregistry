package agent

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var PublishCmd = &cobra.Command{
	Use:   "publish [project-directory]",
	Short: "Publish an agent project to the registry",
	Long: `Publish an agent project to the registry.

Examples:
arctl agent publish ./my-agent`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPublish,
	Example: `arctl agent publish ./my-agent`,
}

func runPublish(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &config.Config{}
	publishCfg := &publishAgentCfg{
		Config: cfg,
	}

	publishCfg.ProjectDir = args[0]

	return publishAgent(publishCfg)
}

type publishAgentCfg struct {
	Config     *config.Config
	ProjectDir string
	Version    string
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

	_, err = apiClient.PublishAgent(jsn)
	if err != nil {
		return fmt.Errorf("failed to publish agent: %w", err)
	}

	fmt.Println("Agent published successfully")
	fmt.Println("You can now run the agent using the following command:")
	fmt.Println("arctl run agent " + jsn.Name + " " + jsn.Version)

	return nil
}
