package agent

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var AddSkillCmd = &cobra.Command{
	Use:   "add-skill <name>",
	Short: "Add a skill to the agent",
	Long: `Add a skill to the agent manifest.

Skills can be added from the following sources:

- A container image (via --image)
- An existing skill registry (via --registry-skill-name)

When starting a new skill from scratch, use 'arctl skill init' instead.

Examples:
  arctl agent add-skill my-skill --image docker.io/org/skill:latest
  arctl agent add-skill my-skill --registry-skill-name cool-skill
`,
	Args: cobra.ExactArgs(1),
	RunE: runAddSkill,
}

var (
	skillProjectDir           string
	skillImage                string
	skillRegistryURL          string
	skillRegistrySkillName    string
	skillRegistrySkillVersion string
)

func init() {
	AddSkillCmd.Flags().StringVar(&skillProjectDir, "project-dir", ".", "Project directory (default: current directory)")
	AddSkillCmd.Flags().StringVar(&skillImage, "image", "", "Docker image containing the skill")
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

	ref, err := buildSkillRef(name)
	if err != nil {
		return err
	}
	if err := checkDuplicateSkill(manifest, ref.Name); err != nil {
		return err
	}
	manifest.Skills = append(manifest.Skills, ref)
	slices.SortFunc(manifest.Skills, func(a, b models.SkillRef) int {
		return strings.Compare(a.Name, b.Name)
	})

	manager := common.NewManifestManager(resolvedDir)
	if err := manager.Save(manifest); err != nil {
		return fmt.Errorf("failed to save agent.yaml: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("Added skill '%s' to agent.yaml", ref.Name))
	return nil
}

func buildSkillRef(name string) (models.SkillRef, error) {
	hasImage := skillImage != ""
	hasRegistry := skillRegistrySkillName != ""
	if !hasImage && !hasRegistry {
		return models.SkillRef{}, fmt.Errorf("one of --image or --registry-skill-name is required")
	}
	if hasImage && hasRegistry {
		return models.SkillRef{}, fmt.Errorf("only one of --image or --registry-skill-name may be set")
	}
	if hasImage {
		return models.SkillRef{
			Name:  name,
			Image: skillImage,
		}, nil
	}
	return models.SkillRef{
		Name:                 name,
		RegistrySkillName:    skillRegistrySkillName,
		RegistrySkillVersion: skillRegistrySkillVersion,
		RegistryURL:          skillRegistryURL,
	}, nil
}

func checkDuplicateSkill(manifest *models.AgentManifest, name string) error {
	for _, existing := range manifest.Skills {
		if strings.EqualFold(existing.Name, name) {
			return fmt.Errorf("a skill named '%s' already exists in agent.yaml", name)
		}
	}
	return nil
}
