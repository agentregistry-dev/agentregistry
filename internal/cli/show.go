package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var (
	showOutputFormat string
	showVersion      string
)

var showCmd = &cobra.Command{
	Use:   "show <resource-type> <resource-name>",
	Short: "Show details of a resource",
	Long:  `Shows detailed information about a resource (mcp, skill, registry).`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		resourceType := args[0]
		resourceName := args[1]

		// Initialize database
		if err := database.Initialize(); err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer func() {
			if err := database.Close(); err != nil {
				log.Printf("Warning: Failed to close database: %v", err)
			}
		}()

		switch resourceType {
		case "mcp":
			servers := findServersByName(resourceName)
			if len(servers) == 0 {
				fmt.Printf("Server '%s' not found\n", resourceName)
				return
			}

			// Filter by version if specified
			if showVersion != "" {
				var filteredServers []*models.ServerDetail
				for _, s := range servers {
					if s.Version == showVersion {
						filteredServers = append(filteredServers, s)
					}
				}
				if len(filteredServers) == 0 {
					fmt.Printf("Server '%s' with version '%s' not found\n", resourceName, showVersion)
					fmt.Printf("Available versions: ")
					for i, s := range servers {
						if i > 0 {
							fmt.Print(", ")
						}
						fmt.Print(s.Version)
					}
					fmt.Println()
					return
				}
				servers = filteredServers
			}

			// Handle JSON output format
			if showOutputFormat == "json" {
				if len(servers) == 1 {
					// Single server - output as object
					fmt.Println(servers[0].Data)
				} else {
					// Multiple servers - output as array
					fmt.Println("[")
					for i, server := range servers {
						fmt.Print(server.Data)
						if i < len(servers)-1 {
							fmt.Println(",")
						} else {
							fmt.Println()
						}
					}
					fmt.Println("]")
				}
				return
			}

			// Group servers by base name (same server, different versions)
			serverGroups := groupServersByBaseName(servers)

			if len(serverGroups) == 1 && len(serverGroups[0].Servers) == 1 {
				// Single server, single version - show detailed view
				showServerDetails(serverGroups[0].Servers[0], nil)
			} else if len(serverGroups) == 1 {
				// Single server name but multiple versions
				group := serverGroups[0]
				if showVersion == "" {
					// Show latest version with note about other versions
					latest := group.Servers[0] // Assume first is latest or most relevant
					otherVersions := make([]string, 0, len(group.Servers)-1)
					for i := 1; i < len(group.Servers); i++ {
						otherVersions = append(otherVersions, group.Servers[i].Version)
					}
					showServerDetails(latest, otherVersions)
				} else {
					// Specific version requested, show it
					showServerDetails(group.Servers[0], nil)
				}
			} else {
				// Multiple different servers
				fmt.Printf("Found %d servers matching '%s':\n\n", len(serverGroups), resourceName)
				for i, group := range serverGroups {
					fmt.Printf("=== Server %d/%d ===\n", i+1, len(serverGroups))
					if len(group.Servers) > 1 {
						// Multiple versions available
						otherVersions := make([]string, 0, len(group.Servers)-1)
						for j := 1; j < len(group.Servers); j++ {
							otherVersions = append(otherVersions, group.Servers[j].Version)
						}
						showServerDetails(group.Servers[0], otherVersions)
					} else {
						showServerDetails(group.Servers[0], nil)
					}
					if i < len(serverGroups)-1 {
						fmt.Println()
					}
				}
			}

		case "skill":
			skill, err := database.GetSkillByName(resourceName)
			if err != nil {
				log.Fatalf("Failed to get skill: %v", err)
			}
			if skill == nil {
				fmt.Printf("Skill '%s' not found\n", resourceName)
				return
			}

			// Handle JSON output format
			if showOutputFormat == "json" {
				fmt.Println(skill.Data)
				return
			}

			// Display skill details in table format
			t := printer.NewTablePrinter(os.Stdout)
			t.SetHeaders("Property", "Value")
			t.AddRow("Name", skill.Name)
			t.AddRow("Description", skill.Description)
			t.AddRow("Version", skill.Version)
			t.AddRow("Status", printer.FormatStatus(skill.Installed))
			t.AddRow("Registry", skill.RegistryName)
			if err := t.Render(); err != nil {
				printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
			}

		case "registry":
			registry, err := database.GetRegistryByName(resourceName)
			if err != nil {
				log.Fatalf("Failed to get registry: %v", err)
			}
			if registry == nil {
				fmt.Printf("Registry '%s' not found\n", resourceName)
				return
			}

			// Display registry details in table format
			t := printer.NewTablePrinter(os.Stdout)
			t.SetHeaders("Property", "Value")
			t.AddRow("Name", registry.Name)
			t.AddRow("URL", registry.URL)
			t.AddRow("Type", registry.Type)
			t.AddRow("Added", printer.FormatTimestampShort(registry.CreatedAt))
			t.AddRow("Age", printer.FormatAge(registry.CreatedAt))
			if err := t.Render(); err != nil {
				printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
			}

		default:
			fmt.Printf("Unknown resource type: %s\n", resourceType)
			fmt.Println("Valid types: mcp, skill, registry")
		}
	},
}

// showServerDetails displays detailed information about a server
// otherVersions is a list of other available versions (can be nil)
func showServerDetails(server *models.ServerDetail, otherVersions []string) {
	// Parse the stored combined data for additional details
	var combinedData models.CombinedServerData
	var registryType, registryStatus, updatedAt string

	if err := json.Unmarshal([]byte(server.Data), &combinedData); err == nil {
		// Extract registry type
		if len(combinedData.Server.Packages) > 0 {
			registryType = combinedData.Server.Packages[0].RegistryType
		} else if len(combinedData.Server.Remotes) > 0 {
			registryType = combinedData.Server.Remotes[0].Type
		}

		// Extract status
		registryStatus = combinedData.Meta.Official.Status
		if !combinedData.Meta.Official.UpdatedAt.IsZero() {
			updatedAt = printer.FormatAge(combinedData.Meta.Official.UpdatedAt)
		}
	}

	// Use installed status if registry status is not available
	if registryStatus == "" {
		if server.Installed {
			registryStatus = "installed"
		} else {
			registryStatus = "available"
		}
	}

	// Split namespace and name
	namespace, name := splitServerName(server.Name)

	// Display server details in table format
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")
	t.AddRow("Full Name", server.Name)
	t.AddRow("Namespace", printer.EmptyValueOrDefault(namespace, "<none>"))
	t.AddRow("Name", name)
	t.AddRow("Title", printer.EmptyValueOrDefault(server.Title, "<none>"))
	t.AddRow("Description", printer.EmptyValueOrDefault(server.Description, "<none>"))

	// Show version with indicator if other versions exist
	versionDisplay := server.Version
	if len(otherVersions) > 0 {
		versionDisplay = fmt.Sprintf("%s (%d other versions available)", server.Version, len(otherVersions))
	}
	t.AddRow("Version", versionDisplay)

	if len(otherVersions) > 0 {
		versionsStr := ""
		for i, v := range otherVersions {
			if i > 0 {
				versionsStr += ", "
			}
			versionsStr += v
		}
		t.AddRow("Other Versions", versionsStr)
	}

	t.AddRow("Type", printer.EmptyValueOrDefault(registryType, "<none>"))
	t.AddRow("Status", registryStatus)
	t.AddRow("Updated", printer.EmptyValueOrDefault(updatedAt, "<none>"))
	t.AddRow("Registry", server.RegistryName)
	t.AddRow("Website", printer.EmptyValueOrDefault(server.WebsiteURL, "<none>"))
	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}

// ServerVersionGroup groups servers with the same base name but different versions
type ServerVersionGroup struct {
	BaseName string
	Servers  []*models.ServerDetail
}

// groupServersByBaseName groups servers by their base name (ignoring registry prefix differences)
func groupServersByBaseName(servers []*models.ServerDetail) []ServerVersionGroup {
	groups := make(map[string]*ServerVersionGroup)

	for _, server := range servers {
		// Use the full name as the grouping key
		// If servers have the same name from different registries, they'll be in different groups
		key := server.Name

		if group, exists := groups[key]; exists {
			group.Servers = append(group.Servers, server)
		} else {
			groups[key] = &ServerVersionGroup{
				BaseName: server.Name,
				Servers:  []*models.ServerDetail{server},
			}
		}
	}

	// Convert map to slice
	result := make([]ServerVersionGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}

	return result
}

func init() {
	rootCmd.AddCommand(showCmd)
	showCmd.Flags().StringVarP(&showOutputFormat, "output", "o", "table", "Output format (table, json)")
	showCmd.Flags().StringVarP(&showVersion, "version", "v", "", "Show specific version of the server")
}
