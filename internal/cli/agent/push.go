package agent

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var PushCmd = &cobra.Command{
	Use:   "push [project-directory]",
	Short: "Push an agent project to the registry without publishing",
	Long: `Push an agent project to the registry without publishing.
The agent will be created in the registry but will not be marked as published.

Examples:
arctl agent push ./my-agent`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPush,
	Example: `arctl agent push ./my-agent`,
}

func runPush(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &config.Config{}
	pushCfg := &pushAgentCfg{
		Config: cfg,
	}

	pushCfg.ProjectDir = args[0]

	return pushAgent(pushCfg)
}

type pushAgentCfg struct {
	Config     *config.Config
	ProjectDir string
	Version    string
}

func pushAgent(cfg *pushAgentCfg) error {
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
	}

	_, err = apiClient.PushAgent(jsn)
	if err != nil {
		return fmt.Errorf("failed to push agent: %w", err)
	}

	fmt.Printf("Agent '%s' version %s pushed successfully\n", jsn.Name, jsn.Version)

	return nil
}
