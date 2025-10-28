package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/jedib0t/go-pretty/v6/table"
	"github.com/spf13/cobra"
)

var (
	listAll      bool
	listPageSize int
)

var listCmd = &cobra.Command{
	Use:   "list <resource-type>",
	Short: "List resources from connected registries",
	Long:  `Lists resources (mcp, skill, registry) across the connected registries.`,
	Args:  cobra.ExactArgs(1),
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

		resourceType := args[0]

		switch resourceType {
		case "mcp":
			fmt.Println("Listing MCP servers:")
			servers, err := database.GetServers()
			if err != nil {
				log.Fatalf("Failed to get servers: %v", err)
			}
			if len(servers) == 0 {
				fmt.Println("  No MCP servers available")
				fmt.Println("  Connect a registry first: arctl connect <url> <name>")
			} else {
				fmt.Printf("  Found %d servers total\n\n", len(servers))
				displayPaginatedServers(servers, listPageSize, listAll)
			}
		case "skill":
			fmt.Println("Listing skills:")
			skills, err := database.GetSkills()
			if err != nil {
				log.Fatalf("Failed to get skills: %v", err)
			}
			if len(skills) == 0 {
				fmt.Println("  No skills available")
				fmt.Println("  Connect a registry first: arctl connect <url> <name>")
			} else {
				fmt.Printf("  Found %d skills total\n\n", len(skills))
				displayPaginatedSkills(skills, listPageSize, listAll)
			}
		case "registry":
			registries, err := database.GetRegistries()
			if err != nil {
				log.Fatalf("Failed to get registries: %v", err)
			}
			if len(registries) == 0 {
				fmt.Println("No registries connected")
				fmt.Println("Connect a registry: arctl connect <url> <name>")
			} else {
				fmt.Printf("\nConnected Registries (%d)\n\n", len(registries))
				t := table.NewWriter()
				t.SetOutputMirror(os.Stdout)
				t.AppendHeader(table.Row{"Name", "URL", "Type", "Added"})

				for _, r := range registries {
					t.AppendRow(table.Row{
						r.Name,
						r.URL,
						r.Type,
						r.CreatedAt.Format("2006-01-02 15:04"),
					})
				}

				t.SetStyle(table.StyleLight)
				t.Render()
				fmt.Println()
			}
		default:
			fmt.Printf("Unknown resource type: %s\n", resourceType)
			fmt.Println("Valid types: mcp, skill, registry")
		}
	},
}

func displayPaginatedServers(servers []models.ServerDetail, pageSize int, showAll bool) {
	total := len(servers)

	if showAll || total <= pageSize {
		// Show all items
		printServersTable(servers)
		return
	}

	// Paginate
	reader := bufio.NewReader(os.Stdin)
	start := 0

	for start < total {
		end := start + pageSize
		if end > total {
			end = total
		}

		// Display current page
		printServersTable(servers[start:end])

		// Check if there are more items
		remaining := total - end
		if remaining > 0 {
			fmt.Printf("\nShowing %d-%d of %d. ", start+1, end, total)
			fmt.Printf("%d more available. Show more? (y/n/all): ", remaining)

			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\nStopping pagination.")
				return
			}

			response = strings.ToLower(strings.TrimSpace(response))

			switch response {
			case "all", "a":
				// Show all remaining
				fmt.Println()
				printServersTable(servers[end:])
				return
			case "y", "yes":
				// Continue to next page
				start = end
				fmt.Println()
			default:
				// Stop pagination
				return
			}
		} else {
			// No more items
			fmt.Printf("\nShowing all %d items.\n", total)
			return
		}
	}
}

func displayPaginatedSkills(skills []models.Skill, pageSize int, showAll bool) {
	total := len(skills)

	if showAll || total <= pageSize {
		// Show all items
		printSkillsTable(skills)
		return
	}

	// Paginate
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
			fmt.Printf("\nShowing %d-%d of %d. ", start+1, end, total)
			fmt.Printf("%d more available. Show more? (y/n/all): ", remaining)

			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\nStopping pagination.")
				return
			}

			response = strings.ToLower(strings.TrimSpace(response))

			switch response {
			case "all", "a":
				// Show all remaining
				fmt.Println()
				printSkillsTable(skills[end:])
				return
			case "y", "yes":
				// Continue to next page
				start = end
				fmt.Println()
			default:
				// Stop pagination
				return
			}
		} else {
			// No more items
			fmt.Printf("\nShowing all %d items.\n", total)
			return
		}
	}
}

// ServerPackage represents a package in the server data
type ServerPackage struct {
	RegistryType string `json:"registryType"`
	Transport    struct {
		Type string `json:"type"`
	} `json:"transport"`
}

// ServerData represents the full server JSON data
type ServerData struct {
	Packages []ServerPackage `json:"packages"`
}

func printServersTable(servers []models.ServerDetail) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "Title", "Version", "Transport", "Status"})

	for _, s := range servers {
		title := s.Title
		if title == "" {
			title = "-"
		}

		// Parse the server data to extract transport type
		transport := "-"
		var serverData ServerData
		if err := json.Unmarshal([]byte(s.Data), &serverData); err == nil {
			if len(serverData.Packages) > 0 {
				pkg := serverData.Packages[0]
				if pkg.Transport.Type != "" {
					transport = pkg.Transport.Type
				} else if pkg.RegistryType != "" {
					transport = pkg.RegistryType
				}
			}
		}

		status := "available"
		if s.Installed {
			status = "installed"
		}

		t.AppendRow(table.Row{
			truncateString(s.Name, 40),
			truncateString(title, 30),
			s.Version,
			transport,
			status,
		})
	}

	t.SetStyle(table.StyleLight)
	t.Render()
	fmt.Println()
}

func printSkillsTable(skills []models.Skill) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Name", "Description", "Version", "Status"})

	for _, s := range skills {
		status := "available"
		if s.Installed {
			status = "installed"
		}

		t.AppendRow(table.Row{
			truncateString(s.Name, 40),
			truncateString(s.Description, 50),
			s.Version,
			status,
		})
	}

	t.SetStyle(table.StyleLight)
	t.Render()
	fmt.Println()
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}

func init() {
	rootCmd.AddCommand(listCmd)
	listCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all items without pagination")
	listCmd.Flags().IntVarP(&listPageSize, "page-size", "p", 15, "Number of items per page")
}
