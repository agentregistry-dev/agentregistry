package agent

import (
	"fmt"
	"os"

	kagentcli "github.com/kagent-dev/kagent/go/cli/cli/agent"
	kagentconfig "github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var AddMcpCmd = &cobra.Command{
	Use:   "add-mcp [name] [args...]",
	Short: "Add an MCP server entry to agent.yaml",
	Long:  `Add an MCP server entry to agent.yaml. Use flags for non-interactive setup or run without flags to open the wizard.`,
	Args:  cobra.ArbitraryArgs,
	RunE:  runAddMcp,
}

var (
	remoteURL string
	headers   []string
	command   string
	args      []string
	env       []string
	image     string
	build     string
)

func init() {
	AddMcpCmd.Flags().StringVar(&remoteURL, "remote", "", "Remote MCP server URL (http/https)")
	AddMcpCmd.Flags().StringSliceVar(&headers, "header", nil, "HTTP header for remote MCP in KEY=VALUE format (repeatable, supports ${VAR} for env vars)")
	AddMcpCmd.Flags().StringVar(&command, "command", "", "Command to run MCP server (e.g., npx, uvx, arctl, or a binary)")
	AddMcpCmd.Flags().StringSliceVar(&args, "arg", nil, "Command argument (repeatable)")
	AddMcpCmd.Flags().StringSliceVar(&env, "env", nil, "Environment variable in KEY=VALUE format (repeatable)")
	AddMcpCmd.Flags().StringVar(&image, "image", "", "Container image (optional; mutually exclusive with --build)")
	AddMcpCmd.Flags().StringVar(&build, "build", "", "Container build (optional; mutually exclusive with --image)")
}

func runAddMcp(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &kagentconfig.Config{}
	addMcpCfg := &kagentcli.AddMcpCfg{
		Config: cfg,
	}

	if len(args) > 0 {
		addMcpCfg.Name = args[0]
		if len(args) > 1 && addMcpCfg.Command != "" {
			addMcpCfg.Args = append(addMcpCfg.Args, args[1:]...)
		}
	}
	if err := kagentcli.AddMcpCmd(addMcpCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return nil
}
