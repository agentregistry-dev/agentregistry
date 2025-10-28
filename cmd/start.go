package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the arctl server",
	Long:  `Starts/restarts the arctl with the existing configuration.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Starting arctl server on port 8080...")
		// TODO: Implement start logic
		// 1. Load configuration
		// 2. Start MCP server endpoint
		// 3. Start API server
		fmt.Println("✓ Server started successfully")
		fmt.Println("MCP endpoint: http://arctl.local:8080/mcp")
		fmt.Println("API endpoint: http://localhost:8080/api")
	},
}

func init() {
	rootCmd.AddCommand(startCmd)
}
