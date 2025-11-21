package cli

import (
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp"
	"github.com/agentregistry-dev/agentregistry/internal/cli/skill"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "arctl",
	Short: "Agent Registry CLI",
	Long:  `arctl is a CLI tool for managing agents, MCP servers and skills.`,
}

var verbose bool

func Execute() {
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "V", false, "Verbose output")
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(mcp.McpCmd)
	rootCmd.AddCommand(agent.AgentCmd)
	rootCmd.AddCommand(skill.SkillCmd)
}
