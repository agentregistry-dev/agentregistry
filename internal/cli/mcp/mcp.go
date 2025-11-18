package mcp

import (
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
)

var verbose bool
var apiClient *client.Client

func SetAPIClient(client *client.Client) {
	apiClient = client
}

var McpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Commands for managing MCP servers",
	Long:  `Commands for managing MCP servers.`,
	Args:  cobra.ArbitraryArgs,
	Example: `arctl mcp list
arctl mcp show my-mcp-server
arctl mcp publish ./my-mcp-server
arctl mcp deploy my-mcp-server
arctl mcp remove my-mcp-server`,
}

func init() {
	McpCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")

	McpCmd.AddCommand(InitCmd)
	McpCmd.AddCommand(BuildCmd)
	McpCmd.AddCommand(AddToolCmd)
	McpCmd.AddCommand(PublishCmd)
	McpCmd.AddCommand(DeployCmd)
	McpCmd.AddCommand(RemoveCmd)
	McpCmd.AddCommand(ListCmd)
	McpCmd.AddCommand(RunCmd)
	McpCmd.AddCommand(ShowCmd)
}
