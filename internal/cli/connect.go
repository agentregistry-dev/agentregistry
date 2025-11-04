package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry"
	"github.com/spf13/cobra"
)

var (
	fetchOnConnect bool
)

var connectCmd = &cobra.Command{
	Use:   "connect <registry-name> <registry-url>",
	Short: "Connect to a registry",
	Long:  `Connects an existing registry to arctl. Optionally fetches all servers immediately with --fetch.`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		registryName := args[0]
		registryURL := args[1]

		// Initialize database
		if err := database.Initialize(); err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer func() {
			if err := database.Close(); err != nil {
				log.Printf("Warning: Failed to close database: %v", err)
			}
		}()

		fmt.Printf("Connecting to registry: %s (%s)\n", registryName, registryURL)

		// Validate that the URL hosts a registry
		fmt.Print("Validating registry URL... ")
		client := registry.NewClient()
		if err := client.ValidateRegistry(registryURL); err != nil {
			fmt.Println("✗")
			log.Fatalf("Registry validation failed: %v", err)
		}
		fmt.Println("✓")

		// Add the registry to database
		if err := database.AddRegistry(registryName, registryURL, "registry"); err != nil {
			if strings.Contains(err.Error(), "UNIQUE constraint failed") {
				log.Fatalf("Registry '%s' already exists", registryName)
			}
			log.Fatalf("Failed to add registry: %v", err)
		}

		fmt.Println("✓ Registry connected successfully")

		// Fetch data if --fetch flag is provided
		if fetchOnConnect {
			// Get the registry we just added to get its ID
			reg, err := database.GetRegistryByName(registryName)
			if err != nil || reg == nil {
				log.Fatalf("Failed to retrieve registry: %v", err)
			}

			fmt.Printf("\nFetching servers from %s...\n", registryName)
			servers, err := client.FetchAllServers(registryURL, registry.FetchOptions{
				ShowProgress: true,
				Verbose:      false,
			})
			if err != nil {
				log.Fatalf("Failed to fetch servers: %v", err)
			}

			fmt.Printf("Downloaded %d servers, storing in database...\n", len(servers))

			// Store each server
			successCount := 0
			failCount := 0
			for _, serverEntry := range servers {
				server := serverEntry.Server

				// Combine server data and _meta into one JSON structure
				combinedData := map[string]interface{}{
					"server": server,
					"_meta":  json.RawMessage(serverEntry.Meta),
				}

				serverJSON, err := json.Marshal(combinedData)
				if err != nil {
					failCount++
					continue
				}

				err = database.AddOrUpdateServer(
					reg.ID,
					server.Name,
					server.Title,
					server.Description,
					server.Version,
					server.WebsiteURL,
					string(serverJSON),
				)
				if err != nil {
					failCount++
					continue
				}
				successCount++
			}

			fmt.Printf("✓ Stored %d MCP servers", successCount)
			if failCount > 0 {
				fmt.Printf(" (%d failed)", failCount)
			}
			fmt.Println()
			fmt.Println("\nNext step:")
			fmt.Println("  Run 'arctl list mcp' to see available MCP servers")
		} else {
			fmt.Println("\nNext steps:")
			fmt.Println("  Run 'arctl refresh' to fetch registry data")
			fmt.Println("  Run 'arctl list mcp' to see available MCP servers")
		}
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
	connectCmd.Flags().BoolVarP(&fetchOnConnect, "fetch", "f", false, "Automatically fetch all servers from the registry")
}
