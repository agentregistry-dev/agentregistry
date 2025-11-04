package cli

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry"
	"github.com/spf13/cobra"
)

var refreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Refresh data from connected registries",
	Long:  `Updates/fetches the new data from the connected registries.`,
	Run: func(cmd *cobra.Command, args []string) {
		// Initialize database
		if err := database.Initialize(); err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer func() {
			if err := database.Close(); err != nil {
				log.Printf("Warning: Failed to close database: %v", err)
			}
		}()

		fmt.Println("Refreshing data from connected registries...")

		// Get all registries
		registries, err := database.GetRegistries()
		if err != nil {
			log.Fatalf("Failed to get registries: %v", err)
		}

		if len(registries) == 0 {
			fmt.Println("No registries connected. Use 'arctl connect' to add a registry.")
			return
		}

		// Fetch data from each registry
		client := registry.NewClient()
		totalServers := 0

		for _, reg := range registries {
			fmt.Printf("\n📡 Fetching from %s (%s)\n", reg.Name, reg.URL)

			servers, err := client.FetchAllServers(reg.URL, registry.FetchOptions{
				ShowProgress: true,
				Verbose:      false,
			})
			if err != nil {
				fmt.Printf("  ⚠ Failed to fetch data: %v\n", err)
				continue
			}

			fmt.Printf("  📦 Total servers fetched: %d\n", len(servers))

			// Clear existing servers for this registry
			if err := database.ClearRegistryServers(reg.ID); err != nil {
				fmt.Printf("  ⚠ Failed to clear old data: %v\n", err)
				continue
			}

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
					fmt.Printf("  ⚠ Failed to marshal server %s: %v\n", server.Name, err)
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
					fmt.Printf("  ⚠ Failed to store server %s: %v\n", server.Name, err)
					failCount++
					continue
				}
				successCount++
			}

			fmt.Printf("  ✓ Stored %d MCP servers", successCount)
			if failCount > 0 {
				fmt.Printf(" (%d failed)", failCount)
			}
			fmt.Println()
			totalServers += successCount
		}

		fmt.Printf("\n✅ Refresh completed successfully! Total servers: %d\n", totalServers)
	},
}

func init() {
	rootCmd.AddCommand(refreshCmd)
}
