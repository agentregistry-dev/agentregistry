package deployment

import (
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/spf13/cobra"
)

var apiClient *client.Client

func SetAPIClient(c *client.Client) {
	apiClient = c
}

var DeploymentCmd = &cobra.Command{
	Use:     "deployments",
	Aliases: []string{"deploy"},
	Short:   "Manage deployments",
	Long:    `Commands for managing agent and MCP server deployments.`,
	Args:    cobra.ArbitraryArgs,
	Example: `arctl deployments list
arctl deployments create my-agent --type agent
arctl deployments create my-mcp-server --type mcp
arctl deployments delete <deployment-id>`,
}

func init() {
	DeploymentCmd.AddCommand(CreateCmd)
	DeploymentCmd.AddCommand(ListCmd)
	DeploymentCmd.AddCommand(ShowCmd)
	DeploymentCmd.AddCommand(DeleteCmd)
}
