package agent

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
	Use:   "show <agent-name>",
	Short: "Show details of an agent",
	Long:  `Shows detailed information about an agent.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func runShow(cmd *cobra.Command, args []string) error {
	agentName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	agent, err := apiClient.GetAgentByName(agentName)
	if err != nil {
		return fmt.Errorf("failed to get agent: %w", err)
	}
	if agent == nil {
		fmt.Printf("Agent '%s' not found\n", agentName)
		return nil
	}

	// Handle JSON output format
	if showOutputFormat == "json" {
		return outputDataJson(agent)
	}

	// Display agent details in table format
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")
	t.AddRow("Name", agent.Agent.Name)
	t.AddRow("Title", printer.EmptyValueOrDefault(agent.Agent.Title, "<none>"))
	t.AddRow("Description", printer.EmptyValueOrDefault(agent.Agent.Description, "<none>"))
	t.AddRow("Version", agent.Agent.Version)
	t.AddRow("Framework", printer.EmptyValueOrDefault(agent.Agent.Framework, "<none>"))
	t.AddRow("Language", printer.EmptyValueOrDefault(agent.Agent.Language, "<none>"))
	t.AddRow("Model Provider", printer.EmptyValueOrDefault(agent.Agent.ModelProvider, "<none>"))
	t.AddRow("Model Name", printer.EmptyValueOrDefault(agent.Agent.ModelName, "<none>"))
	t.AddRow("Status", agent.Meta.Official.Status)
	t.AddRow("Website", printer.EmptyValueOrDefault(agent.Agent.WebsiteURL, "<none>"))

	if !agent.Meta.Official.PublishedAt.IsZero() {
		t.AddRow("Published", printer.FormatAge(agent.Meta.Official.PublishedAt))
	}
	if !agent.Meta.Official.UpdatedAt.IsZero() {
		t.AddRow("Updated", printer.FormatAge(agent.Meta.Official.UpdatedAt))
	}

	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	return nil
}

func init() {
	ShowCmd.Flags().StringVarP(&showOutputFormat, "output", "o", "table", "Output format (table, json)")
}
