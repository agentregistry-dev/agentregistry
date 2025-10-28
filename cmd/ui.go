package cmd

import (
	"fmt"
	"log"

	"github.com/agentregistry-dev/agentregistry/internal/api"
	"github.com/spf13/cobra"
)

var (
	uiPort string
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Launch the web UI",
	Long:  `Starts a web server hosting the arctl UI.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Starting arctl UI on port %s...\n", uiPort)
		fmt.Printf("Open your browser at: http://localhost:%s\n", uiPort)

		// Start the API server with embedded UI
		if err := api.StartServer(uiPort); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(uiCmd)
	uiCmd.Flags().StringVarP(&uiPort, "port", "p", "8080", "Port to run the UI server on")
}
