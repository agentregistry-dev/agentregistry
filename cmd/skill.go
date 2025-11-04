package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var (
	// Flags for skill publish command
	dockerRegistry string
	dockerOrg      string
	registryName   string
	pushFlag       bool
	multiMode      bool
	dryRunFlag     bool
	platformFlag   string
	skillVersion   string
	dockerTagFlag  string
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage Claude Skills",
	Long:  `Wrap, publish, and manage Claude Skills as Docker images in the registry.`,
}

var skillPublishCmd = &cobra.Command{
	Use:   "publish <skill-folder-path>",
	Short: "Wrap and publish a Claude Skill as a Docker image",
	Long: `Wrap a Claude Skill in a Docker image and publish it to both Docker registry and agent registry.
	
The skill folder must contain a SKILL.md file with proper YAML frontmatter.
Use --multi flag to auto-detect and process multiple skill folders.`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillPublish,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Claude Skills from connected registries",
	Long:  `List all Claude Skills available from connected registries.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runSkillList(cmd, args); err != nil {
			printer.PrintError(fmt.Sprintf("Failed to list skills: %v", err))
			os.Exit(1)
		}
	},
}

var skillShowCmd = &cobra.Command{
	Use:   "show <skill-name>",
	Short: "Show details of a Claude Skill",
	Long:  `Display detailed information about a specific Claude Skill.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := runSkillShow(cmd, args); err != nil {
			printer.PrintError(fmt.Sprintf("Failed to show skill: %v", err))
			os.Exit(1)
		}
	},
}

func runSkillPublish(cmd *cobra.Command, args []string) error {
	skillPath := args[0]

	// Validate path exists
	absPath, err := filepath.Abs(skillPath)
	if err != nil {
		return fmt.Errorf("failed to resolve skill path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("skill path does not exist: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Publishing skill from: %s", absPath))

	// Detect skills
	skills, err := detectSkills(absPath)
	if err != nil {
		return fmt.Errorf("failed to detect skills: %w", err)
	}

	if len(skills) == 0 {
		return fmt.Errorf("no valid skills found at path: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Found %d skill(s) to publish", len(skills)))

	// TODO: Implement the actual publishing logic
	// For each skill:
	// 1. Validate skill structure
	// 2. Build Docker image
	// 3. Push to Docker registry (if --push flag)
	// 4. Publish to agent registry

	for _, skill := range skills {
		printer.PrintInfo(fmt.Sprintf("Processing skill: %s", skill))

		if dryRunFlag {
			printer.PrintInfo("[DRY RUN] Would publish skill: " + skill)
			continue
		}

		// TODO: Implement actual build and publish logic
		printer.PrintWarning("Skill publishing not yet implemented")
	}

	if !dryRunFlag {
		printer.PrintSuccess("Skill publishing complete!")
	}

	return nil
}

func runSkillList(cmd *cobra.Command, args []string) error {
	if APIClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	skills, err := APIClient.GetSkills()
	if err != nil {
		return fmt.Errorf("failed to get skills: %w", err)
	}

	if len(skills) == 0 {
		printer.PrintInfo("No skills found. Connect to a registry or publish a skill.")
		return nil
	}

	// Create table printer
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Name", "Title", "Version", "Category", "Registry", "Installed")

	for _, skill := range skills {
		installedStatus := "No"
		if skill.Installed {
			installedStatus = "Yes"
		}

		title := skill.Title
		if title == "" {
			title = "-"
		}

		category := skill.Category
		if category == "" {
			category = "-"
		}

		t.AddRow(
			skill.Name,
			title,
			skill.Version,
			category,
			skill.RegistryName,
			installedStatus,
		)
	}

	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	return nil
}

func runSkillShow(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if APIClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	skill, err := APIClient.GetSkillByName(skillName)
	if err != nil {
		return fmt.Errorf("failed to get skill: %w", err)
	}

	if skill == nil {
		return fmt.Errorf("skill '%s' not found", skillName)
	}

	// Display skill details in table format
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")

	t.AddRow("Name", skill.Name)
	t.AddRow("Title", printer.EmptyValueOrDefault(skill.Title, "<none>"))
	t.AddRow("Description", skill.Description)
	t.AddRow("Version", skill.Version)
	t.AddRow("Category", printer.EmptyValueOrDefault(skill.Category, "<none>"))
	t.AddRow("Registry", skill.RegistryName)

	installedStatus := "No"
	if skill.Installed {
		installedStatus = "Yes"
	}
	t.AddRow("Installed", installedStatus)

	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	// Print raw data if available
	if skill.Data != "" {
		fmt.Println("\nRaw Data:")
		fmt.Println(skill.Data)
	}

	return nil
}

// detectSkills scans the given path for skill folders
// If multiMode is true, it looks for subdirectories containing SKILL.md
// Otherwise, it expects the path itself to be a skill folder
func detectSkills(path string) ([]string, error) {
	var skills []string

	// Check if path contains SKILL.md directly (single skill mode)
	skillMdPath := filepath.Join(path, "SKILL.md")
	if _, err := os.Stat(skillMdPath); err == nil {
		// Single skill found
		return []string{path}, nil
	}

	// Multi mode: scan subdirectories for SKILL.md
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		subPath := filepath.Join(path, entry.Name())
		skillMdPath := filepath.Join(subPath, "SKILL.md")

		if _, err := os.Stat(skillMdPath); err == nil {
			skills = append(skills, subPath)
		}
	}
	if len(skills) == 0 {
		return nil, errors.New("SKILL.md not found in this folder or in any immediate subfolder")
	}
	return skills, nil
}

func init() {
	// Add subcommands to skill command
	skillCmd.AddCommand(skillPublishCmd)
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillShowCmd)

	// Flags for publish command
	skillPublishCmd.Flags().StringVar(&dockerRegistry, "docker-registry", "docker.io", "Docker registry URL")
	skillPublishCmd.Flags().StringVar(&dockerOrg, "docker-org", "", "Docker organization/namespace (required)")
	skillPublishCmd.Flags().StringVar(&registryName, "registry", "", "Target agent registry name")
	skillPublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries")
	skillPublishCmd.Flags().BoolVar(&multiMode, "multi", false, "Auto-detect and process multiple skills in subdirectories")
	skillPublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	skillPublishCmd.Flags().StringVar(&platformFlag, "platform", "linux/amd64", "Target platform(s) for Docker build (e.g., linux/amd64,linux/arm64)")
	skillPublishCmd.Flags().StringVar(&skillVersion, "version", "", "Override version from SKILL.md metadata")
	skillPublishCmd.Flags().StringVar(&dockerTagFlag, "tag", "", "Additional Docker tag (can be specified multiple times)")

	_ = skillPublishCmd.MarkFlagRequired("docker-org")

	// Flags for list command
	skillListCmd.Flags().StringVar(&registryName, "registry", "", "Filter by registry name")

	// Add skill command to root
	rootCmd.AddCommand(skillCmd)
}
