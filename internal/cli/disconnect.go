package cli

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"
)

var disconnectCmd = &cobra.Command{
	Use:   "disconnect <registry-name>",
	Short: "Disconnect a registry",
	Long:  `Removes the cached data and the registry from the config.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		registryName := args[0]

		if APIClient == nil {
			log.Fatalf("API client not initialized")
		}

		fmt.Printf("Disconnecting registry: %s\n", registryName)

		// Remove registry from database
		// This will also remove associated servers and skills via CASCADE
		if err := APIClient.RemoveRegistry(registryName); err != nil {
			log.Fatalf("Failed to disconnect registry: %v", err)
		}

		fmt.Println("✓ Registry disconnected successfully")
	},
}

func init() {
	rootCmd.AddCommand(disconnectCmd)
}
