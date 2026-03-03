package prompt

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var (
	showOutputFormat string
)

var ShowCmd = &cobra.Command{
	Use:   "show <prompt-name>",
	Short: "Show details of a prompt",
	Long:  `Shows detailed information about a prompt from the registry.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	ShowCmd.Flags().StringVarP(&showOutputFormat, "output", "o", "table", "Output format (table, json)")
}

func runShow(cmd *cobra.Command, args []string) error {
	promptName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	prompt, err := apiClient.GetPromptByName(promptName)
	if err != nil {
		return fmt.Errorf("failed to get prompt: %w", err)
	}

	if prompt == nil {
		fmt.Printf("Prompt '%s' not found\n", promptName)
		return nil
	}

	if showOutputFormat == "json" {
		p := printer.New(printer.OutputTypeJSON, false)
		if err := p.PrintJSON(prompt); err != nil {
			return fmt.Errorf("failed to output JSON: %w", err)
		}
		return nil
	}

	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")
	t.AddRow("Name", prompt.Prompt.Name)
	t.AddRow("Description", prompt.Prompt.Description)
	t.AddRow("Version", prompt.Prompt.Version)
	if prompt.Meta.Official != nil {
		t.AddRow("Status", prompt.Meta.Official.Status)
	}

	// Show a preview of the content (first 200 chars)
	content := prompt.Prompt.Content
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	t.AddRow("Content", content)

	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	return nil
}
