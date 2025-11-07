package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var (
	listAll      bool
	listAllTypes bool
	listPageSize int
	filterType   string
	sortBy       string
	outputFormat string
)

var listCmd = &cobra.Command{
	Use:   "list [resource-type]",
	Short: "List resources from connected registries",
	Long:  `Lists resources (mcp, skill, registry) across the connected registries. Use -A to list all resource types.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if APIClient == nil {
			log.Fatalf("API client not initialized")
		}

		// If -A flag is used and no resource type is specified, list all types
		if listAllTypes && len(args) == 0 {
			listAllResourceTypes()
			return
		}

		// If -A is used with a resource type, treat it as -a (no pagination)
		if listAllTypes && len(args) == 1 {
			listAll = true
		}

		// Require resource type if -A is not used
		if len(args) == 0 {
			fmt.Println("Error: resource type required (or use -A to list all types)")
			fmt.Println("Valid types: mcp, skill, registry")
			fmt.Println("Usage: arctl list <resource-type> or arctl list -A")
			return
		}

		resourceType := strings.ToLower(args[0])

		switch resourceType {
		case "mcp", "mcp-servers", "tools":
			servers, err := APIClient.GetServers()
			if err != nil {
				log.Fatalf("Failed to get servers: %v", err)
			}

			// Filter by type if specified
			if filterType != "" {
				servers = filterServersByType(servers, filterType)
			}

			if len(servers) == 0 {
				if filterType != "" {
					fmt.Printf("No MCP servers found with type '%s'\n", filterType)
				} else {
					fmt.Println("No MCP servers available")
				}
			} else {
				// Handle different output formats
				switch outputFormat {
				case "json":
					outputDataJson(servers)
				case "yaml":
					outputDataYaml(servers)
				default:
					displayPaginatedServers(servers, listPageSize, listAll)
				}
			}
		case "skill", "skills":
			skills, err := APIClient.GetSkills()
			if err != nil {
				log.Fatalf("Failed to get skills: %v", err)
			}
			if len(skills) == 0 {
				fmt.Println("No skills available")
			} else {
				// Handle different output formats
				switch outputFormat {
				case "json":
					outputDataJson(skills)
				case "yaml":
					outputDataYaml(skills)
				default:
					displayPaginatedSkills(skills, listPageSize, listAll)
				}
			}
		case "agent", "agents":
			agents, err := APIClient.GetAgents()
			if err != nil {
				log.Fatalf("Failed to get skills: %v", err)
			}
			if len(agents) == 0 {
				fmt.Println("No skills available")
			} else {
				// Handle different output formats
				switch outputFormat {
				case "json":
					outputDataJson(agents)
				case "yaml":
					outputDataYaml(agents)
				default:
					panic("not implemented")
				}
			}
		case "registry", "registries":
			registries, err := APIClient.GetRegistries()
			if err != nil {
				log.Fatalf("Failed to get registries: %v", err)
			}
			if len(registries) == 0 {
				fmt.Println("No registries connected")
			} else {
				// Handle different output formats
				switch outputFormat {
				case "json":
					outputDataJson(registries)
				case "yaml":
					outputDataYaml(registries)
				default:
					t := printer.NewTablePrinter(os.Stdout)
					t.SetHeaders("Name", "URL", "Type", "Age")

					for _, r := range registries {
						t.AddRow(
							r.Name,
							r.URL,
							r.Type,
							printer.FormatAge(r.CreatedAt),
						)
					}

					if err := t.Render(); err != nil {
						printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
					}
				}
			}
		default:
			fmt.Printf("Unknown resource type: %s\n", resourceType)
			fmt.Println("Valid types: mcp, skill, registry")
		}
	},
}

func displayPaginatedServers(servers []models.ServerDetail, pageSize int, showAll bool) {
	// Group servers by name to handle multiple versions
	serverGroups := groupServersByName(servers)
	total := len(serverGroups)

	if showAll || total <= pageSize {
		// Show all items
		printServersTable(serverGroups)
		return
	}

	// Simple pagination with Enter to continue
	reader := bufio.NewReader(os.Stdin)
	start := 0

	for start < total {
		end := start + pageSize
		if end > total {
			end = total
		}

		// Display current page
		printServersTable(serverGroups[start:end])

		// Check if there are more items
		remaining := total - end
		if remaining > 0 {
			fmt.Printf("\nShowing %d-%d of %d servers. %d more available.\n", start+1, end, total, remaining)
			fmt.Print("Press Enter to continue, 'a' for all, or 'q' to quit: ")

			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\nStopping pagination.")
				return
			}

			response = strings.TrimSpace(strings.ToLower(response))

			switch response {
			case "a", "all":
				// Show all remaining
				fmt.Println()
				printServersTable(serverGroups[end:])
				return
			case "q", "quit":
				// Quit pagination
				fmt.Println()
				return
			default:
				// Enter or any other key continues to next page
				start = end
				fmt.Println()
			}
		} else {
			// No more items
			fmt.Printf("\nShowing all %d servers.\n", total)
			return
		}
	}
}

// ServerGroup represents a server with potentially multiple versions
type ServerGroup struct {
	Server        models.ServerDetail
	VersionCount  int
	LatestVersion string
	Namespace     string
	Name          string
}

// groupServersByName groups servers by name and picks the latest version
func groupServersByName(servers []models.ServerDetail) []ServerGroup {
	groups := make(map[string]*ServerGroup)

	for _, s := range servers {
		if existing, ok := groups[s.Name]; ok {
			existing.VersionCount++
			// Keep the latest version (assumes servers are sorted by version DESC from DB)
			// We keep the first one we see since it should be the latest
		} else {
			// Split namespace and name
			namespace, name := splitServerName(s.Name)

			groups[s.Name] = &ServerGroup{
				Server:        s,
				VersionCount:  1,
				LatestVersion: s.Version,
				Namespace:     namespace,
				Name:          name,
			}
		}
	}

	// Convert map to slice
	result := make([]ServerGroup, 0, len(groups))
	for _, group := range groups {
		result = append(result, *group)
	}

	// Sort the results
	sortServerGroups(result, sortBy)

	return result
}

// sortServerGroups sorts server groups by the specified column
func sortServerGroups(groups []ServerGroup, column string) {
	column = strings.ToLower(column)

	switch column {
	case "namespace":
		// Sort by namespace, then name
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				if groups[i].Namespace > groups[j].Namespace ||
					(groups[i].Namespace == groups[j].Namespace && groups[i].Name > groups[j].Name) {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	case "version":
		// Sort by version
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				if groups[i].LatestVersion > groups[j].LatestVersion {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	case "type":
		// Sort by registry type
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				typeI := getServerType(groups[i].Server)
				typeJ := getServerType(groups[j].Server)
				if typeI > typeJ {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	case "status":
		// Sort by status
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				statusI := getServerStatus(groups[i].Server)
				statusJ := getServerStatus(groups[j].Server)
				if statusI > statusJ {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	case "updated":
		// Sort by updated time (most recent first)
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				timeI := getServerUpdatedTime(groups[i].Server)
				timeJ := getServerUpdatedTime(groups[j].Server)
				if timeI.Before(timeJ) {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	default:
		// Default: sort by name
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				if groups[i].Name > groups[j].Name {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	}
}

// Helper functions to extract server properties for sorting
func getServerType(server models.ServerDetail) string {
	var combinedData models.CombinedServerData
	if err := json.Unmarshal([]byte(server.Data), &combinedData); err != nil {
		return ""
	}

	if len(combinedData.Server.Packages) > 0 {
		return combinedData.Server.Packages[0].RegistryType
	} else if len(combinedData.Server.Remotes) > 0 {
		return combinedData.Server.Remotes[0].Type
	}
	return ""
}

func getServerStatus(server models.ServerDetail) string {
	var combinedData models.CombinedServerData
	if err := json.Unmarshal([]byte(server.Data), &combinedData); err != nil {
		return ""
	}
	return combinedData.Meta.Official.Status
}

func getServerUpdatedTime(server models.ServerDetail) time.Time {
	var combinedData models.CombinedServerData
	if err := json.Unmarshal([]byte(server.Data), &combinedData); err != nil {
		return time.Time{}
	}
	return combinedData.Meta.Official.UpdatedAt
}

func displayPaginatedSkills(skills []models.Skill, pageSize int, showAll bool) {
	total := len(skills)

	if showAll || total <= pageSize {
		// Show all items
		printSkillsTable(skills)
		return
	}

	// Simple pagination with Enter to continue
	reader := bufio.NewReader(os.Stdin)
	start := 0

	for start < total {
		end := start + pageSize
		if end > total {
			end = total
		}

		// Display current page
		printSkillsTable(skills[start:end])

		// Check if there are more items
		remaining := total - end
		if remaining > 0 {
			fmt.Printf("\nShowing %d-%d of %d skills. %d more available.\n", start+1, end, total, remaining)
			fmt.Print("Press Enter to continue, 'a' for all, or 'q' to quit: ")

			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\nStopping pagination.")
				return
			}

			response = strings.TrimSpace(strings.ToLower(response))

			switch response {
			case "a", "all":
				// Show all remaining
				fmt.Println()
				printSkillsTable(skills[end:])
				return
			case "q", "quit":
				// Quit pagination
				fmt.Println()
				return
			default:
				// Enter or any other key continues to next page
				start = end
				fmt.Println()
			}
		} else {
			// No more items
			fmt.Printf("\nShowing all %d skills.\n", total)
			return
		}
	}
}
func printServersTable(serverGroups []ServerGroup) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Namespace", "Name", "Version", "Type", "Status", "Updated")

	for _, group := range serverGroups {
		s := group.Server

		// Parse the stored combined data
		var combinedData models.CombinedServerData
		registryType := "<none>"
		registryStatus := "<none>"
		updatedAt := ""

		if err := json.Unmarshal([]byte(s.Data), &combinedData); err == nil {
			// Extract registry type from packages or remotes
			if len(combinedData.Server.Packages) > 0 {
				registryType = combinedData.Server.Packages[0].RegistryType
			} else if len(combinedData.Server.Remotes) > 0 {
				registryType = combinedData.Server.Remotes[0].Type
			}

			// Extract status from _meta
			registryStatus = combinedData.Meta.Official.Status
			if !combinedData.Meta.Official.UpdatedAt.IsZero() {
				updatedAt = printer.FormatAge(combinedData.Meta.Official.UpdatedAt)
			}
		}

		// Use installed status if registry status is not available
		if registryStatus == "" || registryStatus == "<none>" {
			if s.Installed {
				registryStatus = "installed"
			} else {
				registryStatus = "available"
			}
		}

		// Format version display
		versionDisplay := group.LatestVersion
		if group.VersionCount > 1 {
			versionDisplay = fmt.Sprintf("%s (+%d)", group.LatestVersion, group.VersionCount-1)
		}

		// Use empty string if no namespace
		namespace := group.Namespace
		if namespace == "" {
			namespace = "<none>"
		}

		t.AddRow(
			printer.TruncateString(namespace, 30),
			printer.TruncateString(group.Name, 40),
			versionDisplay,
			registryType,
			registryStatus,
			updatedAt,
		)
	}

	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}

func printSkillsTable(skills []models.Skill) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Name", "Description", "Version", "Status")

	for _, s := range skills {
		status := printer.FormatStatus(s.Installed)

		t.AddRow(
			printer.TruncateString(s.Name, 40),
			printer.TruncateString(s.Description, 50),
			s.Version,
			status,
		)
	}

	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}

// filterServersByType filters servers by their registry type
func filterServersByType(servers []models.ServerDetail, typeFilter string) []models.ServerDetail {
	typeFilter = strings.ToLower(typeFilter)
	var filtered []models.ServerDetail

	for _, s := range servers {
		var combinedData models.CombinedServerData
		if err := json.Unmarshal([]byte(s.Data), &combinedData); err != nil {
			continue
		}

		// Extract registry type from packages or remotes
		serverType := ""
		if len(combinedData.Server.Packages) > 0 {
			serverType = strings.ToLower(combinedData.Server.Packages[0].RegistryType)
		} else if len(combinedData.Server.Remotes) > 0 {
			serverType = strings.ToLower(combinedData.Server.Remotes[0].Type)
		}

		if serverType == typeFilter {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

// listAllResourceTypes lists all resource types (mcp, skill, registry)
func listAllResourceTypes() {
	fmt.Println("=== MCP Servers ===")
	servers, err := APIClient.GetServers()
	if err != nil {
		log.Fatalf("Failed to get servers: %v", err)
	}

	// Filter by type if specified
	if filterType != "" {
		servers = filterServersByType(servers, filterType)
	}

	if len(servers) == 0 {
		if filterType != "" {
			fmt.Printf("No MCP servers found with type '%s'\n", filterType)
		} else {
			fmt.Println("No MCP servers available")
		}
	} else {
		displayPaginatedServers(servers, listPageSize, true) // Always show all when listing all types
	}

	fmt.Println("\n=== Skills ===")
	skills, err := APIClient.GetSkills()
	if err != nil {
		log.Fatalf("Failed to get skills: %v", err)
	}
	if len(skills) == 0 {
		fmt.Println("No skills available")
	} else {
		displayPaginatedSkills(skills, listPageSize, true) // Always show all when listing all types
	}

	fmt.Println("\n=== Registries ===")
	registries, err := APIClient.GetRegistries()
	if err != nil {
		log.Fatalf("Failed to get registries: %v", err)
	}
	if len(registries) == 0 {
		fmt.Println("No registries connected")
	} else {
		t := printer.NewTablePrinter(os.Stdout)
		t.SetHeaders("Name", "URL", "Type", "Age")

		for _, r := range registries {
			t.AddRow(
				r.Name,
				r.URL,
				r.Type,
				printer.FormatAge(r.CreatedAt),
			)
		}

		if err := t.Render(); err != nil {
			printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
		}
	}
}

func outputDataJson[T any](data []T) {
	p := printer.New(printer.OutputTypeJSON, false)
	if err := p.PrintJSON(data); err != nil {
		log.Fatalf("Failed to output JSON: %v", err)
	}
}

func outputDataYaml[T any](data []T) {
	// For now, YAML is not implemented, fallback to JSON
	fmt.Println("YAML output not yet implemented, using JSON:")
	outputDataJson(data)
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all items without pagination (for specific resource type)")
	listCmd.Flags().BoolVarP(&listAllTypes, "All", "A", false, "List all resource types (mcp, skill, registry)")
	listCmd.Flags().IntVarP(&listPageSize, "page-size", "p", 15, "Number of items per page")
	listCmd.Flags().StringVarP(&filterType, "type", "t", "", "Filter by registry type (e.g., npm, pypi, oci, sse, streamable-http)")
	listCmd.Flags().StringVarP(&sortBy, "sortBy", "s", "name", "Sort by column (name, namespace, version, type, status, updated)")
	listCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json, yaml, wide)")
}
