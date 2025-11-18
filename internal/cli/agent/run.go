package agent

import (
	"fmt"
	"os"

	kagentcli "github.com/kagent-dev/kagent/go/cli/cli/agent"
	kagentconfig "github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var RunCmd = &cobra.Command{
	Use:   "run [project-directory-or-agent-name]",
	Short: "Run agent project locally with docker-compose and launch chat interface",
	Long: `Run an agent project locally using docker-compose and launch an interactive chat session.

You can provide either a local directory path or an agent name from the registry.

Examples:
  arctl agent run ./my-agent        # Run from local directory
  arctl agent run .                 # Run from current directory
  arctl agent run dice              # Run agent 'dice' from registry`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
	Example: `arctl agent run ./my-agent
  arctl agent run dice`,
}

func runRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &kagentconfig.Config{}
	runCfg := &kagentcli.RunCfg{
		Config: cfg,
	}

	link := args[0]
	if _, err := os.Stat(link); err == nil {
		runCfg.ProjectDir = link
		fmt.Println("Running agent from local directory: ", link)
		if err := kagentcli.RunCmd(cmd.Context(), runCfg); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	} else {
		// Assume this is an agent name from the registry
		agentModel, err := apiClient.GetAgentByName(link)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		manifest := agentModel.Agent.AgentManifest
		if err := kagentcli.RunRemote(cmd.Context(), runCfg.Config, &manifest); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	}

	return nil
}
