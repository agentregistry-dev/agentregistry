package skill

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var (
	listAll      bool
	listPageSize int
	outputFormat string
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List skills",
	Long:  `List skills from connected registries.`,
	RunE:  runList,
}

func init() {
	ListCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all items without pagination")
	ListCmd.Flags().IntVarP(&listPageSize, "page-size", "p", 15, "Number of items per page")
	ListCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json, yaml)")
}

func runList(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	skills, err := apiClient.GetSkills()
	if err != nil {
		return fmt.Errorf("failed to get skills: %w", err)
	}

	if len(skills) == 0 {
		fmt.Println("No skills available")
		return nil
	}

	// Handle different output formats
	switch outputFormat {
	case "json":
		return outputDataJson(skills)
	case "yaml":
		return outputDataYaml(skills)
	default:
		displayPaginatedSkills(skills, listPageSize, listAll)
	}

	return nil
}

func displayPaginatedSkills(skills []*models.SkillResponse, pageSize int, showAll bool) {
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
		end := min(start+pageSize, total)

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

func printSkillsTable(skills []*models.SkillResponse) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Name", "Title", "Version", "Category", "Published", "Website")

	for _, s := range skills {
		publishedStatus := "False"
		if s.Meta.Official.Published {
			publishedStatus = "True"
		}

		t.AddRow(
			printer.TruncateString(s.Skill.Name, 40),
			printer.TruncateString(s.Skill.Title, 40),
			s.Skill.Version,
			printer.EmptyValueOrDefault(s.Skill.Category, "<none>"),
			publishedStatus,
			s.Skill.WebsiteURL,
		)
	}

	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
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
