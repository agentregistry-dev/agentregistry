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
	ListCmd.Flags().StringVarP(&sortBy, "sortBy", "s", "name", "Sort by column (name, version, type, status, updated)")
	ListCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json, yaml)")
}

func runList(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	servers, err := apiClient.GetPublishedServers()
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
	// Sort servers before displaying
	sortServers(servers, sortBy)
	total := len(servers)

	if showAll || total <= pageSize {
		// Show all items
		printServersTable(servers, deployedServers)
		return
	}

	// Simple pagination with Enter to continue
	reader := bufio.NewReader(os.Stdin)
	start := 0

	for start < total {
		end := min(start+pageSize, total)

		// Display current page
		printServersTable(servers[start:end], deployedServers)

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
				printServersTable(servers[end:], deployedServers)
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

// sortServers sorts servers by the specified column
func sortServers(servers []*v0.ServerResponse, column string) {
	column = strings.ToLower(column)

	switch column {
	case "version":
		// Sort by version
		for i := range len(servers) {
			for j := i + 1; j < len(servers); j++ {
				if servers[i].Server.Version > servers[j].Server.Version {
					servers[i], servers[j] = servers[j], servers[i]
				}
			}
		}
	case "type":
		// Sort by registry type
		for i := range len(servers) {
			for j := i + 1; j < len(servers); j++ {
				typeI := ""
				typeJ := ""
				if len(servers[i].Server.Packages) > 0 {
					typeI = servers[i].Server.Packages[0].RegistryType
				} else if len(servers[i].Server.Remotes) > 0 {
					typeI = servers[i].Server.Remotes[0].Type
				}
				if len(servers[j].Server.Packages) > 0 {
					typeJ = servers[j].Server.Packages[0].RegistryType
				} else if len(servers[j].Server.Remotes) > 0 {
					typeJ = servers[j].Server.Remotes[0].Type
				}
				if typeI > typeJ {
					servers[i], servers[j] = servers[j], servers[i]
				}
			}
		}
	case "status":
		// Sort by status
		for i := range len(servers) {
			for j := i + 1; j < len(servers); j++ {
				statusI := string(servers[i].Meta.Official.Status)
				statusJ := string(servers[j].Meta.Official.Status)
				if statusI > statusJ {
					servers[i], servers[j] = servers[j], servers[i]
				}
			}
		}
	case "updated":
		// Sort by updated time (most recent first)
		for i := range len(servers) {
			for j := i + 1; j < len(servers); j++ {
				timeI := servers[i].Meta.Official.UpdatedAt
				timeJ := servers[j].Meta.Official.UpdatedAt
				if timeI.Before(timeJ) {
					servers[i], servers[j] = servers[j], servers[i]
				}
			}
		}
	default:
		// Default: sort by name, then version
		for i := range len(servers) {
			for j := i + 1; j < len(servers); j++ {
				if servers[i].Server.Name > servers[j].Server.Name ||
					(servers[i].Server.Name == servers[j].Server.Name && servers[i].Server.Version > servers[j].Server.Version) {
					servers[i], servers[j] = servers[j], servers[i]
				}
			}
		}
	}
}

func printServersTable(servers []*v0.ServerResponse, deployedServers []*client.DeploymentResponse) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Name", "Version", "Type", "Published", "Deployed", "Updated")

	// Create a map of deployed servers by name and version
	deployedMap := make(map[string]map[string]*client.DeploymentResponse)
	for _, d := range deployedServers {
		if deployedMap[d.ServerName] == nil {
			deployedMap[d.ServerName] = make(map[string]*client.DeploymentResponse)
		}
		deployedMap[d.ServerName][d.Version] = d
	}

	for _, s := range servers {
		// Parse the stored combined data
		registryType := "<none>"
		updatedAt := ""

		// Extract registry type from packages or remotes
		if len(s.Server.Packages) > 0 {
			registryType = s.Server.Packages[0].RegistryType
		} else if len(s.Server.Remotes) > 0 {
			registryType = s.Server.Remotes[0].Type
		}

		// Extract published status using the published boolean field
		publishedStatus := "False"
		isPublished, err := isServerPublished(s.Server.Name, s.Server.Version)
		if err != nil {
			log.Printf("Warning: Failed to check if server is published: %v", err)
		}
		if isPublished {
			publishedStatus = "True"
		}
		if !s.Meta.Official.UpdatedAt.IsZero() {
			updatedAt = printer.FormatAge(s.Meta.Official.UpdatedAt)
		}

		// Use the full server name (includes namespace if present)
		fullName := s.Server.Name

		deployedStatus := "False"
		if serverDeployments, ok := deployedMap[s.Server.Name]; ok {
			if _, ok := serverDeployments[s.Server.Version]; ok {
				deployedStatus = "True"
			}
			// If this specific version is not deployed, show False even if another version is deployed
		}

		t.AddRow(
			printer.TruncateString(fullName, 50),
			s.Server.Version,
			registryType,
			publishedStatus,
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

func outputDataJson(data any) error {
	p := printer.New(printer.OutputTypeJSON, false)
	if err := p.PrintJSON(data); err != nil {
		return fmt.Errorf("failed to output JSON: %w", err)
	}
	return nil
}

func outputDataYaml(data any) error {
	// For now, YAML is not implemented, fallback to JSON
	fmt.Println("YAML output not yet implemented, using JSON:")
	return outputDataJson(data)
}
