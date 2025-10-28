package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/spf13/cobra"
)

// RegistryResponse represents the response from the MCP registry
type RegistryResponse struct {
	Servers  []ServerEntry    `json:"servers"`
	Metadata RegistryMetadata `json:"metadata"`
}

// RegistryMetadata contains pagination information
type RegistryMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor"`
}

// ServerEntry represents a server entry in the registry
type ServerEntry struct {
	Server ServerSpec             `json:"server"`
	Meta   map[string]interface{} `json:"_meta"`
}

// ServerSpec represents the server specification
type ServerSpec struct {
	Name        string              `json:"name"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Version     string              `json:"version"`
	Status      string              `json:"status"`
	WebsiteURL  string              `json:"websiteUrl"`
	Repository  Repository          `json:"repository"`
	Packages    []ServerPackageInfo `json:"packages"`
}

// ServerPackageInfo represents package information from the server spec
type ServerPackageInfo struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier"`
	Version      string `json:"version"`
	Transport    struct {
		Type string `json:"type"`
	} `json:"transport"`
}

// Repository represents the repository information
type Repository struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

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
		totalServers := 0
		for _, registry := range registries {
			fmt.Printf("\n📡 Fetching from %s (%s)\n", registry.Name, registry.URL)

			servers, err := fetchRegistryData(registry.URL)
			if err != nil {
				fmt.Printf("  ⚠ Failed to fetch data: %v\n", err)
				continue
			}

			fmt.Printf("  📦 Total servers fetched: %d\n", len(servers))

			// Clear existing servers for this registry
			if err := database.ClearRegistryServers(registry.ID); err != nil {
				fmt.Printf("  ⚠ Failed to clear old data: %v\n", err)
				continue
			}

			// Store each server
			successCount := 0
			failCount := 0
			for _, serverEntry := range servers {
				server := serverEntry.Server

				// Marshal the full server spec back to JSON for storage
				serverJSON, err := json.Marshal(server)
				if err != nil {
					fmt.Printf("  ⚠ Failed to marshal server %s: %v\n", server.Name, err)
					failCount++
					continue
				}

				err = database.AddOrUpdateServer(
					registry.ID,
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

func fetchRegistryData(baseURL string) ([]ServerEntry, error) {
	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	var allServers []ServerEntry
	cursor := ""
	pageCount := 0
	const pageLimit = 100

	// Fetch all pages using cursor-based pagination
	for {
		pageCount++

		// Build URL with pagination parameters
		fetchURL := fmt.Sprintf("%s?limit=%d", baseURL, pageLimit)
		if cursor != "" {
			// URL-encode the cursor value
			fetchURL = fmt.Sprintf("%s&cursor=%s", fetchURL, url.QueryEscape(cursor))
		}

		fmt.Printf("    Fetching page %d...\n", pageCount)

		// Fetch registry data
		resp, err := client.Get(fetchURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", pageCount, err)
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("unexpected status code on page %d: %d", pageCount, resp.StatusCode)
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response on page %d: %w", pageCount, err)
		}

		// Parse JSON
		var registryResp RegistryResponse
		if err := json.Unmarshal(body, &registryResp); err != nil {
			return nil, fmt.Errorf("failed to parse JSON on page %d: %w", pageCount, err)
		}

		// Filter servers by status (only keep "active" servers as recommended in docs)
		activeServers := make([]ServerEntry, 0, len(registryResp.Servers))
		for _, server := range registryResp.Servers {
			// Include servers with "active" status or empty status (for backward compatibility)
			if server.Server.Status == "" || server.Server.Status == "active" {
				activeServers = append(activeServers, server)
			}
		}

		allServers = append(allServers, activeServers...)
		fmt.Printf("    Found %d active servers on this page\n", len(activeServers))

		// Check if there are more pages
		if registryResp.Metadata.NextCursor == "" {
			break
		}

		cursor = registryResp.Metadata.NextCursor
	}

	return allServers, nil
}

func init() {
	rootCmd.AddCommand(refreshCmd)
}
