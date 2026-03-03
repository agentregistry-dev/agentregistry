package prompt

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var (
	listAll      bool
	listPageSize int
	outputFormat string
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List prompts",
	Long:  `List prompts from connected registries.`,
	RunE:  runList,
}

func init() {
	ListCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all items without pagination")
	ListCmd.Flags().IntVarP(&listPageSize, "page-size", "p", 15, "Number of items per page")
	ListCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json)")
}

func runList(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	prompts, err := apiClient.GetPrompts()
	if err != nil {
		return fmt.Errorf("failed to get prompts: %w", err)
	}

	if len(prompts) == 0 {
		fmt.Println("No prompts available")
		return nil
	}

	switch outputFormat {
	case "json":
		p := printer.New(printer.OutputTypeJSON, false)
		if err := p.PrintJSON(prompts); err != nil {
			return fmt.Errorf("failed to output JSON: %w", err)
		}
	default:
		displayPaginatedPrompts(prompts, listPageSize, listAll)
	}

	return nil
}

func displayPaginatedPrompts(prompts []*models.PromptResponse, pageSize int, showAll bool) {
	total := len(prompts)

	if showAll || total <= pageSize {
		printPromptsTable(prompts)
		return
	}

	reader := bufio.NewReader(os.Stdin)
	start := 0

	for start < total {
		end := min(start+pageSize, total)

		printPromptsTable(prompts[start:end])

		remaining := total - end
		if remaining > 0 {
			fmt.Printf("\nShowing %d-%d of %d prompts. %d more available.\n", start+1, end, total, remaining)
			fmt.Print("Press Enter to continue, 'a' for all, or 'q' to quit: ")

			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\nStopping pagination.")
				return
			}

			response = strings.TrimSpace(strings.ToLower(response))

			switch response {
			case "a", "all":
				fmt.Println()
				printPromptsTable(prompts[end:])
				return
			case "q", "quit":
				fmt.Println()
				return
			default:
				start = end
				fmt.Println()
			}
		} else {
			fmt.Printf("\nShowing all %d prompts.\n", total)
			return
		}
	}
}

func printPromptsTable(prompts []*models.PromptResponse) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Name", "Version", "Description")

	for _, p := range prompts {
		t.AddRow(
			printer.TruncateString(p.Prompt.Name, 40),
			p.Prompt.Version,
			printer.TruncateString(p.Prompt.Description, 60),
		)
	}

	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}
