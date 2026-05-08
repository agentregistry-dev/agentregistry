package mcp

import (
	"github.com/spf13/cobra"
)

var verbose bool

var McpCmd = &cobra.Command{
	Use:     "mcp",
	Short:   "Commands for managing MCP servers",
	Long:    `Commands for managing MCP servers.`,
	Args:    cobra.ArbitraryArgs,
	Example: `arctl mcp add-tool my-tool`,
}

func init() {
	McpCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	McpCmd.AddCommand(AddToolCmd)
}
