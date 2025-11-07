package agent

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/kagent-dev/kagent/go/cli/agent/frameworks/common"
	"github.com/kagent-dev/kagent/go/cli/cli/agent"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
	Long:  "Manage agents",
}

func NewAgentCmd(cli *client.Client) *cobra.Command {

	cfg := &config.Config{}

	initCfg := &agent.InitCfg{
		Config: cfg,
	}

	initCmd := &cobra.Command{
		Use:   "init [framework] [language] [agent-name]",
		Short: "Initialize a new agent project",
		Long: `Initialize a new agent project using the specified framework and language.

You can customize the root agent instructions using the --instruction-file flag.
You can select a specific model using --model-provider and --model-name flags.
If no custom instruction file is provided, a default dice-rolling instruction will be used.
If no model is specified, the agent will need to be configured later.

Examples:
  arctl agent init adk python dice
  arctl agent init adk python dice --instruction-file instructions.md
  arctl agent init adk python dice --model-provider Gemini --model-name gemini-2.0-flash`,
		Args: cobra.ExactArgs(3),
		Run: func(cmd *cobra.Command, args []string) {
			initCfg.Framework = args[0]
			initCfg.Language = args[1]
			initCfg.AgentName = args[2]

			if err := agent.InitCmd(initCfg, "arctl agent", "0.7.4"); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `arctl agent init adk python dice`,
	}

	// Add flags for custom instructions and model selection
	initCmd.Flags().StringVar(&initCfg.InstructionFile, "instruction-file", "", "Path to file containing custom instructions for the root agent")
	initCmd.Flags().StringVar(&initCfg.ModelProvider, "model-provider", "Gemini", "Model provider (OpenAI, Anthropic, Gemini)")
	initCmd.Flags().StringVar(&initCfg.ModelName, "model-name", "gemini-2.0-flash", "Model name (e.g., gpt-4, claude-3-5-sonnet, gemini-2.0-flash)")
	initCmd.Flags().StringVar(&initCfg.Description, "description", "", "Description for the agent")

	runCfg := &agent.RunCfg{
		Config: cfg,
	}

	runCmd := &cobra.Command{
		Use:   "run [project-directory]",
		Short: "Run agent project locally with docker-compose and launch chat interface",
		Long: `Run an agent project locally using docker-compose and launch an interactive chat session.

Examples:
  arctl agent run ./my-agent
  arctl agent run .`,
		Args: cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) > 0 {
				runCfg.ProjectDir = args[0]
			} else {
				runCfg.ProjectDir = "."
			}

			if err := agent.RunCmd(cmd.Context(), runCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `arctl agent run ./my-agent`,
	}

	runCmd.Flags().StringVar(&runCfg.ProjectDir, "project-dir", "", "Project directory (default: current directory)")

	buildCfg := &agent.BuildCfg{
		Config: cfg,
	}

	buildCmd := &cobra.Command{
		Use:   "build [project-directory]",
		Short: "Build a Docker images for an agent project",
		Long: `Build Docker images for an agent project created with the init command.

This command will look for a agent.yaml file in the specified project directory and build Docker images using docker build. The images can optionally be pushed to a registry.

Image naming:
- If --image is provided, it will be used as the full image specification (e.g., ghcr.io/myorg/my-agent:v1.0.0)
- Otherwise, defaults to localhost:5001/{agentName}:latest where agentName is loaded from agent.yaml

Examples:
  arctl agent build ./my-agent
  arctl agent build ./my-agent --image ghcr.io/myorg/my-agent:v1.0.0
  arctl agent build ./my-agent --image ghcr.io/myorg/my-agent:v1.0.0 --push`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			buildCfg.ProjectDir = args[0]

			if err := agent.BuildCmd(buildCfg); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
		Example: `arctl agent build ./my-agent`,
	}

	// Add flags for build command
	buildCmd.Flags().StringVar(&buildCfg.Image, "image", "", "Full image specification (e.g., ghcr.io/myorg/my-agent:v1.0.0)")
	buildCmd.Flags().BoolVar(&buildCfg.Push, "push", false, "Push the image to the registry")
	buildCmd.Flags().StringVar(&buildCfg.Platform, "platform", "", "Target platform for Docker build (e.g., linux/amd64, linux/arm64)")

	publishCfg := &PublishCfg{
		Config: cfg,
	}

	publishCmd := &cobra.Command{
		Use:   "publish [project-directory]",
		Short: "Publish an agent project to the registry",
		Long: `Publish an agent project to the registry.

Examples:
  arctl agent publish ./my-agent`,
		Args: cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			publishCfg.ProjectDir = args[0]

			if err := PublishCmd(publishCfg, cli); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	agentCmd.AddCommand(initCmd, runCmd, buildCmd, publishCmd)

	return agentCmd
}

type PublishCfg struct {
	Config     *config.Config
	ProjectDir string
	Version    string
}

func PublishCmd(cfg *PublishCfg, cli *client.Client) error {
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
		Name:        manifest.Name,
		Version:     version,
		Description: manifest.Description,
	}

	_, err = cli.PublishAgent(jsn)
	if err != nil {
		return fmt.Errorf("failed to publish agent: %w", err)
	}

	return nil
}
