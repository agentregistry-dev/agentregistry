package agent

import (
	"fmt"
	"os"

	"github.com/kagent-dev/kagent/go/cli/cli/agent"
	"github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage agents",
	Long:  "Manage agents",
}

func NewAgentCmd() *cobra.Command {

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

			if err := agent.InitCmd(initCfg); err != nil {
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

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run an agent",
		Long:  "Run an agent",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := &agent.RunCfg{
				Config: &config.Config{},
			}
			agent.RunCmd(cmd.Context(), cfg)
			fmt.Println("Running agent...")
		},
	}

	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Build an agent",
		Long:  "Build an agent",
		Run: func(cmd *cobra.Command, args []string) {
			cfg := &agent.BuildCfg{
				Config: &config.Config{},
			}
			agent.BuildCmd(cfg)
			fmt.Println("Building agent...")
		},
	}

	agentCmd.AddCommand(initCmd, runCmd, buildCmd)

	return agentCmd
}
