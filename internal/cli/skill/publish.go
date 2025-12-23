package skill

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/config"
	"github.com/spf13/cobra"
)

var (
	// Flags for skill publish command
	dockerUrl    string
	dockerTag    string
	platformFlag string
	pushFlag     bool
	dryRunFlag   bool
)

var PublishCmd = &cobra.Command{
	Use:   "publish <skill-folder-path>",
	Short: "Wrap and publish a Claude Skill as a Docker image",
	Long: `Wrap a Claude Skill in a Docker image and publish it to both Docker registry and agent registry.
	
The skill folder must contain a SKILL.md file with proper YAML frontmatter.
Use --multi flag to auto-detect and process multiple skill folders.`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

func init() {
	// Flags for publish command
	PublishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL. For example: docker.io/myorg. The final image name will be <docker-url>/<skill-name>:<tag>")
	PublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PublishCmd.Flags().StringVar(&dockerTag, "tag", "latest", "Docker image tag to use")
	PublishCmd.Flags().StringVar(&platformFlag, "platform", "", "Target platform(s) for the build (e.g., linux/amd64, linux/arm64, or linux/amd64,linux/arm64)")

	_ = PublishCmd.MarkFlagRequired("docker-url")
}

func runPublish(cmd *cobra.Command, args []string) error {
	skillPath := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Detect skills
	skills, err := getSkills(skillPath)
	if err != nil {
		return fmt.Errorf("failed to detect skills: %w", err)
	}

	var errs []error

	for _, skill := range skills {
		printer.PrintInfo(fmt.Sprintf("Processing skill: %s", skill))

		imageRef, skillJson, err := buildSkillDockerImage(skill, dryRunFlag)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to build skill '%s': %w", skill, err))
			continue
		}

		if pushFlag {
			if err := pushSkillDockerImage(imageRef, dryRunFlag); err != nil {
				errs = append(errs, fmt.Errorf("failed to push docker image: %w", err))
				continue
			}
		}

		if dryRunFlag {
			j, _ := json.Marshal(skillJson)
			printer.PrintInfo("[DRY RUN] Would publish skill to registry " + apiClient.BaseURL + ": " + string(j))
		} else {
			// Push to registry (unpublished)
			_, err = apiClient.PushSkill(skillJson)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to push skill '%s': %w", skill, err))
				continue
			}

			// Auto-approve the skill (if configured)
			if config.GetAutoApprove() {
				if err := apiClient.ApproveSkillStatus(skillJson.Name, skillJson.Version, "Auto-approved via publish command"); err != nil {
					errs = append(errs, fmt.Errorf("failed to approve skill '%s': %w", skill, err))
					continue
				}
			}

			// Publish the skill
			if err := apiClient.PublishSkillStatus(skillJson.Name, skillJson.Version); err != nil {
				errs = append(errs, fmt.Errorf("failed to publish skill '%s': %w", skill, err))
				continue
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors occurred during publishing: %w", errors.Join(errs...))
	}

	if !dryRunFlag {
		printer.PrintSuccess("Skill publishing complete!")
	}

	return nil
}
