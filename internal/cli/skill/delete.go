package skill

import (
	"fmt"

	"github.com/spf13/cobra"
)

var (
	deleteForceFlag bool
	deleteVersion   string
)

var DeleteCmd = &cobra.Command{
	Use:   "delete <skill-name>",
	Short: "Delete a skill from the registry",
	Long: `Delete a skill from the registry.
The skill must not be published or deployed unless --force is used.

Examples:
  arctl skill delete my-skill --version 1.0.0
  arctl skill delete my-skill --version 1.0.0 --force`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	DeleteCmd.Flags().StringVar(&deleteVersion, "version", "", "Specify the version to delete (required)")
	DeleteCmd.Flags().BoolVar(&deleteForceFlag, "force", false, "Force delete even if published or deployed")
	_ = DeleteCmd.MarkFlagRequired("version")
}

func runDelete(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Check if skill is published
	isPublished, err := isSkillPublished(skillName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to check if skill is published: %w", err)
	}

	// Fail if published or deployed unless --force is used
	if !deleteForceFlag {
		if isPublished {
			return fmt.Errorf("skill %s version %s is published. Unpublish it first using 'arctl skill unpublish %s --version %s', or use --force to delete anyway", skillName, deleteVersion, skillName, deleteVersion)
		}
	}

	// Delete the skill
	fmt.Printf("Deleting skill %s version %s...\n", skillName, deleteVersion)
	err = apiClient.DeleteSkill(skillName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to delete skill: %w", err)
	}

	fmt.Printf("Skill '%s' version %s deleted successfully\n", skillName, deleteVersion)
	return nil
}

func isSkillPublished(skillName, version string) (bool, error) {
	if apiClient == nil {
		return false, fmt.Errorf("API client not initialized")
	}

	// Get skill using admin endpoint to check published status
	skill, err := apiClient.GetSkillByNameAndVersionAdmin(skillName, version)
	if err != nil {
		return false, err
	}
	if skill == nil {
		return false, nil
	}

	// Check if published field is true
	if skill.Meta.Official != nil && skill.Meta.Official.Published {
		return true, nil
	}

	return false, nil
}
