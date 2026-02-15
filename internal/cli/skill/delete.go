package skill

import (
	"fmt"

	"github.com/spf13/cobra"
)

var deleteVersion string

var DeleteCmd = &cobra.Command{
	Use:   "delete <skill-name>",
	Short: "Delete a skill from the registry",
	Long: `Delete a skill from the registry.

Examples:
  arctl skill delete my-skill --version 1.0.0`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	DeleteCmd.Flags().StringVar(&deleteVersion, "version", "", "Specify the version to delete (required)")
	_ = DeleteCmd.MarkFlagRequired("version")
}

func runDelete(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Delete the skill
	fmt.Printf("Deleting skill %s version %s...\n", skillName, deleteVersion)
	err := apiClient.DeleteSkill(skillName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to delete skill: %w", err)
	}

	fmt.Printf("Skill '%s' version %s deleted successfully\n", skillName, deleteVersion)
	return nil
}
