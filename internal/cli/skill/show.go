package skill

import (
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var (
	showOutputFormat string
)

var ShowCmd = &cobra.Command{
	Use:   "show <skill-name>",
	Short: "Show details of a skill",
	Long:  `Shows detailed information about a skill from the registry.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runShow,
}

func init() {
	ShowCmd.Flags().StringVarP(&showOutputFormat, "output", "o", "table", "Output format (table, json)")
}

func runShow(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	skill, err := apiClient.GetSkillByName(skillName)
	if err != nil {
		return fmt.Errorf("failed to get skill: %w", err)
	}

	if skill == nil {
		fmt.Printf("Skill '%s' not found\n", skillName)
		return nil
	}

	// Handle JSON output format
	if showOutputFormat == "json" {
		fmt.Println(skill)
		return nil
	}

	// Display skill details in table format
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")
	t.AddRow("Name", skill.Skill.Name)
	t.AddRow("Description", skill.Skill.Description)
	t.AddRow("Version", skill.Skill.Version)
	t.AddRow("Category", skill.Skill.Category)
	t.AddRow("Status", skill.Meta.Official.Status)
	t.AddRow("Website", skill.Skill.WebsiteURL)
	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	return nil
}
