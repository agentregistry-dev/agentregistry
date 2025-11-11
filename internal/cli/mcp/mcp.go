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
	Use: "mcp",
}

func init() {
	McpCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose output")
}
