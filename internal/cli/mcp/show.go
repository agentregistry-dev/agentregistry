package mcp

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/printer"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/spf13/cobra"
)

var (
	showOutputFormat string
	showVersion      string
)

var ShowCmd = &cobra.Command{
	Use:   "show <server-name>",
	Short: "Show details of an MCP server",
	Long:  `Shows detailed information about an MCP server.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	ShowCmd.Flags().StringVarP(&showOutputFormat, "output", "o", "table", "Output format (table, json)")
	ShowCmd.Flags().StringVar(&showVersion, "version", "", "Show specific version of the server")
}

func runShow(cmd *cobra.Command, args []string) error {
	serverName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	servers := findServersByName(serverName)
	if len(servers) == 0 {
		fmt.Printf("Server '%s' not found\n", serverName)
		return nil
	}

	// Filter by version if specified
	if showVersion != "" {
		var filteredServers []*v0.ServerResponse
		for _, s := range servers {
			if s.Server.Version == showVersion {
				filteredServers = append(filteredServers, s)
			}
		}
		if len(filteredServers) == 0 {
			fmt.Printf("Server '%s' with version '%s' not found\n", serverName, showVersion)
			fmt.Printf("Available versions: ")
			for i, s := range servers {
				if i > 0 {
					fmt.Print(", ")
				}
				fmt.Print(s.Server.Version)
			}
			fmt.Println()
			return nil
		}
		servers = filteredServers
	}

	// Handle JSON output format
	if showOutputFormat == "json" {
		if len(servers) == 1 {
			// Single server - output as object
			fmt.Println(servers[0])
		} else {
			// Multiple servers - output as array
			fmt.Println("[")
			for i, server := range servers {
				fmt.Print(server)
				if i < len(servers)-1 {
					fmt.Println(",")
				} else {
					fmt.Println()
				}
			}
			fmt.Println("]")
		}
		return nil
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
				otherVersions = append(otherVersions, group.Servers[i].Server.Version)
			}
			showServerDetails(latest, otherVersions)
		} else {
			// Specific version requested, show it
			showServerDetails(group.Servers[0], nil)
		}
	} else {
		// Multiple different servers
		fmt.Printf("Found %d servers matching '%s':\n\n", len(serverGroups), serverName)
		for i, group := range serverGroups {
			fmt.Printf("=== Server %d/%d ===\n", i+1, len(serverGroups))
			if len(group.Servers) > 1 {
				// Multiple versions available
				otherVersions := make([]string, 0, len(group.Servers)-1)
				for j := 1; j < len(group.Servers); j++ {
					otherVersions = append(otherVersions, group.Servers[j].Server.Version)
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

	return nil
}

// showServerDetails displays detailed information about a server
// otherVersions is a list of other available versions (can be nil)
func showServerDetails(server *v0.ServerResponse, otherVersions []string) {
	// Parse the stored combined data for additional details
	var registryType, registryStatus, updatedAt string

	// Extract registry type
	if len(server.Server.Packages) > 0 {
		registryType = server.Server.Packages[0].RegistryType
	} else if len(server.Server.Remotes) > 0 {
		registryType = server.Server.Remotes[0].Type
	}

	// Extract status
	registryStatus = string(server.Meta.Official.Status)
	if !server.Meta.Official.UpdatedAt.IsZero() {
		updatedAt = printer.FormatAge(server.Meta.Official.UpdatedAt)
	}

	// Display server details in table format
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")
	t.AddRow("Full Name", server.Server.Name)
	t.AddRow("Title", printer.EmptyValueOrDefault(server.Server.Title, "<none>"))
	t.AddRow("Description", printer.EmptyValueOrDefault(server.Server.Description, "<none>"))

	// Show version with indicator if other versions exist
	versionDisplay := server.Server.Version
	if len(otherVersions) > 0 {
		versionDisplay = fmt.Sprintf("%s (%d other versions available)", server.Server.Version, len(otherVersions))
	}
	t.AddRow("Version", versionDisplay)

	if len(otherVersions) > 0 {
		t.AddRow("Other Versions", strings.Join(otherVersions, ", "))
	}

	t.AddRow("Type", printer.EmptyValueOrDefault(registryType, "<none>"))
	t.AddRow("Status", registryStatus)
	t.AddRow("Updated", printer.EmptyValueOrDefault(updatedAt, "<none>"))
	t.AddRow("Website", printer.EmptyValueOrDefault(server.Server.WebsiteURL, "<none>"))
	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}

// ServerVersionGroup groups servers with the same base name but different versions
type ServerVersionGroup struct {
	BaseName string
	Servers  []*v0.ServerResponse
}

// groupServersByBaseName groups servers by their base name (ignoring registry prefix differences)
func groupServersByBaseName(servers []*v0.ServerResponse) []ServerVersionGroup {
	groups := make(map[string]*ServerVersionGroup)

	for _, server := range servers {
		// Use the full name as the grouping key
		// If servers have the same name from different registries, they'll be in different groups
		key := server.Server.Name

		if group, exists := groups[key]; exists {
			group.Servers = append(group.Servers, server)
		} else {
			groups[key] = &ServerVersionGroup{
				BaseName: server.Server.Name,
				Servers:  []*v0.ServerResponse{server},
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

func findServersByName(searchName string) []*v0.ServerResponse {
	servers, err := apiClient.GetPublishedServers()
	if err != nil {
		log.Fatalf("Failed to get servers: %v", err)
	}

	// First, try exact match with full name
	for _, s := range servers {
		if s.Server.Name == searchName {
			return []*v0.ServerResponse{s}
		}
	}

	// If no exact match, search for name part (after /)
	var matches []*v0.ServerResponse
	searchLower := strings.ToLower(searchName)

	for _, s := range servers {
		// Extract name part (after /)
		parts := strings.Split(s.Server.Name, "/")
		var namePart string
		if len(parts) == 2 {
			namePart = strings.ToLower(parts[1])
		} else {
			namePart = strings.ToLower(s.Server.Name)
		}

		if namePart == searchLower {
			serverCopy := s
			matches = append(matches, serverCopy)
		}
	}

	return matches
}
