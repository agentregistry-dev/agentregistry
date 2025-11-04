package cli

import (
	"fmt"
	"log"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/spf13/cobra"
)

var disconnectCmd = &cobra.Command{
	Use:   "disconnect <registry-name>",
	Short: "Disconnect a registry",
	Long:  `Removes the cached data and the registry from the config.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		registryName := args[0]

		// Initialize database
		if err := database.Initialize(); err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer func() {
			if err := database.Close(); err != nil {
				log.Printf("Warning: Failed to close database: %v", err)
			}
		}()

		fmt.Printf("Disconnecting registry: %s\n", registryName)

		// Remove registry from database
		// This will also remove associated servers and skills via CASCADE
		if err := database.RemoveRegistry(registryName); err != nil {
			log.Fatalf("Failed to disconnect registry: %v", err)
		}

		fmt.Println("✓ Registry disconnected successfully")
	},
}

func init() {
	rootCmd.AddCommand(disconnectCmd)
}
