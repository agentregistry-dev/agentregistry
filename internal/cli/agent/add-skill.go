package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/agentregistry-dev/agentregistry/internal/cli/skill/templates"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/spf13/cobra"
)

var AddSkillCmd = &cobra.Command{
	Use:   "add-skill <name>",
	Short: "Add a skill to the agent",
	Long: `Add a skill to the agent manifest. Skills can be added from:
  - A Docker image (--image)
  - The skill registry (--registry-skill-name)
  - A new local scaffold (--scaffold)

Examples:
  arctl agent add-skill my-skill --image docker.io/org/skill:latest
  arctl agent add-skill my-skill --registry-skill-name cool-skill
  arctl agent add-skill my-skill --scaffold`,
	Args: cobra.ExactArgs(1),
	RunE: runAddSkill,
}

var (
	skillProjectDir           string
	skillImage                string
	skillScaffold             bool
	skillRegistryURL          string
	skillRegistrySkillName    string
	skillRegistrySkillVersion string
)

func init() {
	AddSkillCmd.Flags().StringVar(&skillProjectDir, "project-dir", ".", "Project directory (default: current directory)")
	AddSkillCmd.Flags().StringVar(&skillImage, "image", "", "Docker image containing the skill")
	AddSkillCmd.Flags().BoolVar(&skillScaffold, "scaffold", false, "Scaffold an empty skill directory within the agent project")
	AddSkillCmd.Flags().StringVar(&skillRegistryURL, "registry-url", "", "Registry URL for pulling the skill")
	AddSkillCmd.Flags().StringVar(&skillRegistrySkillName, "registry-skill-name", "", "Skill name in the registry")
	AddSkillCmd.Flags().StringVar(&skillRegistrySkillVersion, "registry-skill-version", "", "Version of the skill to pull from the registry")
}

func runAddSkill(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := addSkillCmd(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return nil
}

func addSkillCmd(name string) error {
	resolvedDir, err := project.ResolveProjectDir(skillProjectDir)
	if err != nil {
		return err
	}

	manifest, err := project.LoadManifest(resolvedDir)
	if err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Loaded manifest for agent '%s' from %s\n", manifest.Name, resolvedDir)
	}

	// Determine skill type from flags
	var ref models.SkillRef
	ref.Name = name

	hasImage := skillImage != ""
	hasScaffold := skillScaffold
	hasRegistry := skillRegistrySkillName != ""

	flagCount := 0
	if hasImage {
		flagCount++
	}
	if hasScaffold {
		flagCount++
	}
	if hasRegistry {
		flagCount++
	}

	if flagCount == 0 {
		return fmt.Errorf("one of --image, --scaffold, or --registry-skill-name is required")
	}
	if flagCount > 1 {
		return fmt.Errorf("only one of --image, --scaffold, or --registry-skill-name may be set")
	}

	switch {
	case hasImage:
		ref.Image = skillImage
	case hasRegistry:
		ref.RegistrySkillName = skillRegistrySkillName
		ref.RegistrySkillVersion = skillRegistrySkillVersion
		ref.RegistryURL = skillRegistryURL
	case hasScaffold:
		ref.Path = filepath.Join("skills", name)
	}

	// Check for duplicate skill names
	for _, existing := range manifest.Skills {
		if strings.EqualFold(existing.Name, ref.Name) {
			return fmt.Errorf("a skill named '%s' already exists in agent.yaml", ref.Name)
		}
	}

	// Append and validate
	manifest.Skills = append(manifest.Skills, ref)
	manager := common.NewManifestManager(resolvedDir)

	if err := manager.Validate(manifest); err != nil {
		return fmt.Errorf("invalid skill configuration: %w", err)
	}

	if err := manager.Save(manifest); err != nil {
		return fmt.Errorf("failed to save agent.yaml: %w", err)
	}

	// Post-processing: scaffold the skill directory if --scaffold was used
	if hasScaffold {
		if err := scaffoldSkill(resolvedDir, name); err != nil {
			return fmt.Errorf("failed to scaffold skill: %w", err)
		}
		if verbose {
			fmt.Printf("Scaffolded skill at %s\n", filepath.Join(resolvedDir, "skills", name))
		}
	}

	fmt.Printf("Added skill '%s' to agent.yaml\n", ref.Name)
	return nil
}

// scaffoldSkill creates a new empty skill directory within the agent project.
// The generator writes to {ProjectName}/ relative to CWD, so we chdir to the
// skills/ subdirectory first and use the skill name as ProjectName.
func scaffoldSkill(projectDir string, name string) error {
	skillsDir := filepath.Join(projectDir, "skills")
	if err := os.MkdirAll(skillsDir, 0o755); err != nil {
		return fmt.Errorf("failed to create skills directory: %w", err)
	}

	// Save and restore working directory
	origDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}
	if err := os.Chdir(skillsDir); err != nil {
		return fmt.Errorf("failed to chdir to skills directory: %w", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck

	return templates.NewGenerator().GenerateProject(templates.ProjectConfig{
		NoGit:       true,
		Directory:   filepath.Join(skillsDir, name),
		Verbose:     false,
		ProjectName: name,
		Empty:       true,
	})
}
