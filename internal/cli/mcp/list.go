package mcp

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/spf13/cobra"
)

var (
	listAll      bool
	listPageSize int
	filterType   string
	sortBy       string
	outputFormat string
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List MCP servers",
	Long:  `List MCP servers from connected registries.`,
	RunE:  runList,
}

func init() {
	ListCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all items without pagination")
	ListCmd.Flags().IntVarP(&listPageSize, "page-size", "p", 15, "Number of items per page")
	ListCmd.Flags().StringVarP(&filterType, "type", "t", "", "Filter by registry type (e.g., npm, pypi, oci, sse, streamable-http)")
	ListCmd.Flags().StringVarP(&sortBy, "sortBy", "s", "name", "Sort by column (name, namespace, version, type, status, updated)")
	ListCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json, yaml)")
}

func runList(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	servers, err := apiClient.GetServers()
	if err != nil {
		return fmt.Errorf("failed to get servers: %w", err)
	}

	deployedServers, err := apiClient.GetDeployedServers()
	if err != nil {
		log.Printf("Warning: Failed to get deployed servers: %v", err)
		deployedServers = nil
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
		return nil
	}

	// Handle different output formats
	switch outputFormat {
	case "json":
		return outputDataJson(servers)
	case "yaml":
		return outputDataYaml(servers)
	default:
		displayPaginatedServers(servers, deployedServers, listPageSize, listAll)
	}

	return nil
}

func displayPaginatedServers(servers []*v0.ServerResponse, deployedServers []*client.DeploymentResponse, pageSize int, showAll bool) {
	// Group servers by name to handle multiple versions
	serverGroups := groupServersByName(servers)
	total := len(serverGroups)

	if showAll || total <= pageSize {
		// Show all items
		printServersTable(serverGroups, deployedServers)
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
		printServersTable(serverGroups[start:end], deployedServers)

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
				printServersTable(serverGroups[end:], deployedServers)
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
	Server        *v0.ServerResponse
	VersionCount  int
	LatestVersion string
	Namespace     string
	Name          string
}

// groupServersByName groups servers by name and picks the latest version
func groupServersByName(servers []*v0.ServerResponse) []ServerGroup {
	groups := make(map[string]*ServerGroup)

	for _, s := range servers {
		if existing, ok := groups[s.Server.Name]; ok {
			existing.VersionCount++
			// Keep the latest version (assumes servers are sorted by version DESC from DB)
			// We keep the first one we see since it should be the latest
		} else {
			// Split namespace and name
			namespace, name := splitServerName(s.Server.Name)

			groups[s.Server.Name] = &ServerGroup{
				Server:        s,
				VersionCount:  1,
				LatestVersion: s.Server.Version,
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
				typeI := groups[i].Server.Server.Packages[0].RegistryType
				typeJ := groups[j].Server.Server.Packages[0].RegistryType
				if typeI > typeJ {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	case "status":
		// Sort by status
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				statusI := groups[i].Server.Meta.Official.Status
				statusJ := groups[j].Server.Meta.Official.Status
				if statusI > statusJ {
					groups[i], groups[j] = groups[j], groups[i]
				}
			}
		}
	case "updated":
		// Sort by updated time (most recent first)
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				timeI := groups[i].Server.Meta.Official.UpdatedAt
				timeJ := groups[j].Server.Meta.Official.UpdatedAt
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

func printServersTable(serverGroups []ServerGroup, deployedServers []*client.DeploymentResponse) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Namespace", "Name", "Version", "Type", "Status", "Deployed", "Updated")

	deployedMap := make(map[string]*client.DeploymentResponse)
	for _, d := range deployedServers {
		deployedMap[d.ServerName] = d
	}

	for _, group := range serverGroups {
		s := group.Server

		// Parse the stored combined data
		registryType := "<none>"
		updatedAt := ""

		// Extract registry type from packages or remotes
		if len(s.Server.Packages) > 0 {
			registryType = s.Server.Packages[0].RegistryType
		} else if len(s.Server.Remotes) > 0 {
			registryType = s.Server.Remotes[0].Type
		}

		// Extract status from _meta
		registryStatus := string(s.Meta.Official.Status)
		if !s.Meta.Official.UpdatedAt.IsZero() {
			updatedAt = printer.FormatAge(s.Meta.Official.UpdatedAt)
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

		deployedStatus := "-"
		if deployment, ok := deployedMap[s.Server.Name]; ok {
			if deployment.Version == group.LatestVersion {
				deployedStatus = "✓"
			} else {
				deployedStatus = fmt.Sprintf("✓ (v%s)", deployment.Version)
			}
		}

		t.AddRow(
			printer.TruncateString(namespace, 30),
			printer.TruncateString(group.Name, 40),
			versionDisplay,
			registryType,
			registryStatus,
			deployedStatus,
			updatedAt,
		)
	}

	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}

// filterServersByType filters servers by their registry type
func filterServersByType(servers []*v0.ServerResponse, typeFilter string) []*v0.ServerResponse {
	typeFilter = strings.ToLower(typeFilter)
	var filtered []*v0.ServerResponse

	for _, s := range servers {
		// Extract registry type from packages or remotes
		serverType := ""
		if len(s.Server.Packages) > 0 {
			serverType = strings.ToLower(s.Server.Packages[0].RegistryType)
		} else if len(s.Server.Remotes) > 0 {
			serverType = strings.ToLower(s.Server.Remotes[0].Type)
		}

		if serverType == typeFilter {
			filtered = append(filtered, s)
		}
	}

	return filtered
}

func splitServerName(fullName string) (namespace, name string) {
	parts := strings.Split(fullName, "/")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return "", fullName
}

func outputDataJson(data interface{}) error {
	p := printer.New(printer.OutputTypeJSON, false)
	if err := p.PrintJSON(data); err != nil {
		return fmt.Errorf("failed to output JSON: %w", err)
	}
	return nil
}

func outputDataYaml(data interface{}) error {
	// For now, YAML is not implemented, fallback to JSON
	fmt.Println("YAML output not yet implemented, using JSON:")
	return outputDataJson(data)
}
