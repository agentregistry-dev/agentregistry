package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	clicommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/agentregistry-dev/agentregistry/pkg/validators"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/spf13/cobra"
)

var (
	publishVersion   string
	githubRepository string
	dryRunFlag       bool
	overwriteFlag    bool
	publishDesc      string
)

var PublishCmd = &cobra.Command{
	Use:   "publish [agent-name|project-directory]",
	Short: "Publish an agent to the registry",
	Long: `Publish an agent to the registry.

This command supports two modes:

1. From a local project directory (with agent.yaml):
   arctl agent publish ./my-agent

2. Direct registration (without agent.yaml):
   arctl agent publish my-agent --github https://github.com/myorg/my-agent

Agent name format:
  - Must start and end with alphanumeric characters
  - Can contain letters, numbers, dots (.), and hyphens (-)
  - Minimum 2 characters
  - Examples: my-agent, agent.v2, myAgent123

Examples:
  # Publish from current directory (reads metadata from agent.yaml)
  arctl agent publish

  # Publish from specified directory
  arctl agent publish ./my-agent

  # Publish directly with name and GitHub repo (no agent.yaml needed)
  arctl agent publish my-agent \
    --github https://github.com/myorg/my-agent \
    --version 1.0.0 \
    --description "My agent"

  # Show what would be published
  arctl agent publish --dry-run`,
	Args:          cobra.MaximumNArgs(1),
	SilenceUsage:  true,
	SilenceErrors: false,
	RunE:          runPublish,
}

func init() {
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Version to publish (overrides manifest)")
	PublishCmd.Flags().StringVar(&githubRepository, "github", "", "GitHub repository URL")
	PublishCmd.Flags().StringVar(&publishDesc, "description", "", "Agent description (when not using agent.yaml)")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PublishCmd.Flags().BoolVar(&overwriteFlag, "overwrite", false, "Overwrite if the version is already published")
}

func runPublish(cmd *cobra.Command, args []string) error {
	// Default to current directory if no argument provided
	input := "."
	if len(args) > 0 {
		input = args[0]
	}

	// Build AgentJSON from either manifest or direct input
	var agentJSON *models.AgentJSON
	var err error

	absPath, _ := filepath.Abs(input)
	mgr := common.NewManifestManager(absPath)

	if mgr.Exists() {
		agentJSON, err = buildAgentJSONFromManifest(mgr)
	} else {
		agentJSON, err = buildAgentJSONDirect(input)
	}
	if err != nil {
		return err
	}

	// Validate agent name
	if err := validators.ValidateAgentName(agentJSON.Name); err != nil {
		return err
	}

	// Check if agent already exists
	if err := checkAndHandleExistingAgent(agentJSON.Name, agentJSON.Version); err != nil {
		return err
	}

	return publishToRegistry(agentJSON)
}

// buildAgentJSONFromManifest builds AgentJSON from agent.yaml
func buildAgentJSONFromManifest(mgr *common.Manager) (*models.AgentJSON, error) {
	manifest, err := mgr.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load agent.yaml: %w", err)
	}

	version := clicommon.ResolveVersion(publishVersion, manifest.Version)

	// Create a copy without telemetryEndpoint (deployment/runtime concern)
	publishManifest := *manifest
	publishManifest.TelemetryEndpoint = ""

	agentJSON := &models.AgentJSON{
		AgentManifest: publishManifest,
		Version:       version,
		Status:        "active",
	}

	if githubRepository != "" {
		agentJSON.Repository = &model.Repository{
			URL:    githubRepository,
			Source: "github",
		}
	}

	return agentJSON, nil
}

// buildAgentJSONDirect builds AgentJSON from command line flags
func buildAgentJSONDirect(agentName string) (*models.AgentJSON, error) {
	agentName = strings.ToLower(agentName)

	if githubRepository == "" {
		return nil, fmt.Errorf("--github is required when publishing without agent.yaml")
	}
	if publishVersion == "" {
		return nil, fmt.Errorf("--version is required when publishing without agent.yaml")
	}

	return &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:        agentName,
			Description: publishDesc,
		},
		Version: publishVersion,
		Status:  "active",
		Repository: &model.Repository{
			URL:    githubRepository,
			Source: "github",
		},
	}, nil
}

// checkAndHandleExistingAgent checks if an agent version already exists in the registry
// and handles the overwrite logic if needed.
func checkAndHandleExistingAgent(agentName, version string) error {
	printer.PrintInfo(fmt.Sprintf("Publishing agent: %s (v%s)", agentName, version))

	exists, err := isAgentPublished(agentName, version)
	if err != nil {
		return fmt.Errorf("failed to check if agent exists: %w", err)
	}
	if exists {
		if !overwriteFlag {
			return fmt.Errorf("agent %s version %s already exists in the registry. Use --overwrite to replace it", agentName, version)
		}
		printer.PrintInfo(fmt.Sprintf("Overwriting existing agent %s version %s", agentName, version))
		if err := apiClient.DeleteAgent(agentName, version); err != nil {
			return fmt.Errorf("failed to delete existing agent: %w", err)
		}
	}
	return nil
}

// publishToRegistry handles the actual publish or dry-run output.
func publishToRegistry(agentJSON *models.AgentJSON) error {
	if dryRunFlag {
		j, _ := json.MarshalIndent(agentJSON, "", "  ")
		printer.PrintInfo(fmt.Sprintf("[DRY RUN] Would publish agent:\n%s", string(j)))
		return nil
	}

	_, err := apiClient.CreateAgent(agentJSON)
	if err != nil {
		return fmt.Errorf("failed to publish to registry: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("Published: %s (v%s)", agentJSON.Name, agentJSON.Version))
	return nil
}
