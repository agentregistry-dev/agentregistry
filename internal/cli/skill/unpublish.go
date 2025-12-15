package skill

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/spf13/cobra"
)

var (
	unpublishVersion string
	unpublishAll     bool
)

var UnpublishCmd = &cobra.Command{
	Use:   "unpublish <skill-name>",
	Short: "Unpublish a skill",
	Long: `Unpublish a skill from the registry.

This marks the skill as unpublished, hiding it from public listings.
The skill data is not deleted and can be re-published later.

Use --all to unpublish all versions of the skill.`,
	Args: cobra.ExactArgs(1),
	RunE: runUnpublish,
}

func init() {
	UnpublishCmd.Flags().StringVar(&unpublishVersion, "version", "", "Specify the version of the skill to unpublish (defaults to latest)")
	UnpublishCmd.Flags().BoolVar(&unpublishAll, "all", false, "Unpublish all versions of the skill")
}

func runUnpublish(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Validate flags
	if unpublishAll && unpublishVersion != "" {
		return fmt.Errorf("cannot specify both --all and --version flags")
	}

	// If --all flag is set, unpublish all versions
	if unpublishAll {
		return unpublishAllVersions(skillName)
	}

	if unpublishVersion == "" {
		return fmt.Errorf("version is required")
	}

	// Check if the skill with the specific version exists and is published
	// Get all versions to check if this specific version exists and is published
	versions, err := apiClient.GetSkillVersions(skillName)
	if err != nil {
		return fmt.Errorf("failed to get skill versions: %w", err)
	}

	var foundVersion *models.SkillResponse
	for _, v := range versions {
		if v.Skill.Version == unpublishVersion {
			foundVersion = v
			break
		}
	}

	if foundVersion == nil {
		return fmt.Errorf("skill %s version %s not found", skillName, unpublishVersion)
	}

	// Check if it's published
	isPublished := false
	if foundVersion.Meta.Official != nil {
		isPublished = foundVersion.Meta.Official.Published
	}

	if !isPublished {
		return fmt.Errorf("skill %s version %s is not published", skillName, unpublishVersion)
	}

	// Confirm unpublish action
	fmt.Printf("Unpublishing skill: %s (version %s)\n", skillName, unpublishVersion)

	// Call the unpublish API
	if err := apiClient.UnpublishSkill(skillName, unpublishVersion); err != nil {
		return fmt.Errorf("failed to unpublish skill: %w", err)
	}

	fmt.Printf("Skill '%s' version %s unpublished successfully\n", skillName, unpublishVersion)

	return nil
}

func unpublishAllVersions(skillName string) error {
	fmt.Printf("Fetching all versions of %s...\n", skillName)

	// Get all versions of the skill
	versions, err := apiClient.GetSkillVersions(skillName)
	if err != nil {
		return fmt.Errorf("failed to get skill versions: %w", err)
	}

	if len(versions) == 0 {
		return fmt.Errorf("no versions found for skill: %s", skillName)
	}

	fmt.Printf("Found %d version(s)\n\n", len(versions))

	// Unpublish each version
	var failed []string
	var succeeded []string
	for _, version := range versions {
		fmt.Printf("Unpublishing %s version %s...", skillName, version.Skill.Version)
		if err := apiClient.UnpublishSkill(skillName, version.Skill.Version); err != nil {
			fmt.Printf(" ✗ Failed: %v\n", err)
			failed = append(failed, version.Skill.Version)
		} else {
			fmt.Printf(" ✓\n")
			succeeded = append(succeeded, version.Skill.Version)
		}
	}

	// Print summary
	fmt.Println()
	if len(succeeded) > 0 {
		fmt.Printf("✓ Successfully unpublished %d version(s)\n", len(succeeded))
	}
	if len(failed) > 0 {
		fmt.Printf("✗ Failed to unpublish %d version(s)\n", len(failed))
		for _, v := range failed {
			fmt.Printf("  - %s\n", v)
		}
		return fmt.Errorf("some versions failed to unpublish")
	}

	fmt.Println("\nAll versions have been hidden from public listings.")
	fmt.Println("To re-publish them, use: arctl skill publish")

	return nil
}
