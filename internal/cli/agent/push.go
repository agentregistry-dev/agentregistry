package agent

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	arConfig "github.com/agentregistry-dev/agentregistry/pkg/cli/config"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var PushCmd = &cobra.Command{
	Use:   "push [project-directory]",
	Short: "Push an agent project to the registry",
	Long: `Push an agent project to the registry.

This command pushes the agent to the registry (unpublished).

Examples:
arctl agent push ./my-agent`,
	Args:    cobra.ExactArgs(1),
	RunE:    runPush,
	Example: `arctl agent push ./my-agent`,
}

func init() {
	// No flags needed for push
}

func runPush(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	arg := args[0]

	// If the argument is a directory containing an agent project, push from local
	if fi, err := os.Stat(arg); err == nil && fi.IsDir() {
		cfg := &config.Config{}
		pushCfg := &agentCfg{
			Config:     cfg,
			ProjectDir: arg,
			Version:    "latest",
		}
		jsn, err := createAgentJSONFromCfg(pushCfg)
		if err != nil {
			return fmt.Errorf("failed to create agent JSON: %w", err)
		}

		// Push the agent (creates unpublished entry)
		if _, err := apiClient.PushAgent(jsn); err != nil {
			return fmt.Errorf("failed to push agent: %w", err)
		}

		// Auto-approve the agent
		if arConfig.GetAutoApprove() {
			if err := apiClient.ApproveAgentStatus(jsn.Name, jsn.Version, "Auto-approved via push command"); err != nil {
				return fmt.Errorf("failed to approve agent: %w", err)
			}
		}

		return nil
	}

	return fmt.Errorf("invalid argument: %s must be a directory", arg)
}

type agentCfg struct {
	Config     *config.Config
	ProjectDir string
	Version    string
}

func createAgentJSONFromCfg(cfg *agentCfg) (*models.AgentJSON, error) {
	// Validate project directory
	if cfg.ProjectDir == "" {
		return nil, fmt.Errorf("project directory is required")
	}

	// Check if project directory exists
	if _, err := os.Stat(cfg.ProjectDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("project directory does not exist: %s", cfg.ProjectDir)
	}

	version := "latest"
	if cfg.Version != "" {
		version = cfg.Version
	}

	mgr := common.NewManifestManager(cfg.ProjectDir)
	manifest, err := mgr.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load manifest: %w", err)
	}

	return &models.AgentJSON{
		AgentManifest: *manifest,
		Version:       version,
		Status:        "active",
	}, nil
}
