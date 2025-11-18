package agent

import (
	"fmt"
	"os"

	kagentcli "github.com/kagent-dev/kagent/go/cli/cli/agent"
	kagentconfig "github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var InitCmd = &cobra.Command{
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
	Args:    cobra.ExactArgs(3),
	RunE:    runInit,
	Example: `arctl agent init adk python dice`,
}

var (
	initInstructionFile string
	initModelProvider   string
	initModelName       string
	initDescription     string
)

func init() {
	InitCmd.Flags().StringVar(&initInstructionFile, "instruction-file", "", "Path to file containing custom instructions for the root agent")
	InitCmd.Flags().StringVar(&initModelProvider, "model-provider", "Gemini", "Model provider (OpenAI, Anthropic, Gemini)")
	InitCmd.Flags().StringVar(&initModelName, "model-name", "gemini-2.0-flash", "Model name (e.g., gpt-4, claude-3-5-sonnet, gemini-2.0-flash)")
	InitCmd.Flags().StringVar(&initDescription, "description", "", "Description for the agent")
}

func runInit(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &kagentconfig.Config{}
	initCfg := &kagentcli.InitCfg{
		Config: cfg,
	}
	initCfg.Framework = args[0]
	initCfg.Language = args[1]
	initCfg.AgentName = args[2]

	if err := kagentcli.InitCmd(initCfg, "arctl agent", "0.7.4"); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Successfully created agent: %s\n", initCfg.AgentName)
	return nil
}
