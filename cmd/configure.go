package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure <client-name>",
	Short: "Configure a client",
	Long:  `Creates the .json configuration for each client, so it can connect to arctl.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		clientName := args[0]

		fmt.Printf("Configuring client: %s\n", clientName)
		// TODO: Implement configure logic
		// 1. Generate appropriate config for client type
		// 2. Save to appropriate location

		supportedClients := map[string]string{
			"vscode":         ".vscode/mcp.json",
			"cursor":         ".cursor/mcp.json",
			"claude-code":    "claude-code.json",
			"claude-desktop": "claude-desktop.json",
		}

		if configPath, ok := supportedClients[clientName]; ok {
			fmt.Printf("Configuration file: %s\n", configPath)
			fmt.Println(`{"servers":{"ARCTL":{"type":"http", "url": "http://arctl.local:8080/mcp"}}}`)
		} else {
			fmt.Printf("Client '%s' is not supported yet\n", clientName)
			fmt.Println("Supported clients:", "vscode, cursor, claude-code, claude-desktop")
		}
	},
}

func init() {
	rootCmd.AddCommand(configureCmd)
}
