package skill

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var BuildCmd = &cobra.Command{
	Use:   "build <skill-folder-path>",
	Short: "Build a skill as a Docker image",
	Long: `Build a skill from a local folder containing SKILL.md.

This command builds a Docker image for the skill without publishing
it to the registry. Use 'arctl skill publish' to publish afterward.`,
	Args:          cobra.ExactArgs(1),
	RunE:          runSkillBuild,
	SilenceUsage:  true,
	SilenceErrors: false,
	Example: `  arctl skill build ./my-skill
  arctl skill build ./my-skill --docker-url docker.io/myorg
  arctl skill build ./my-skill --docker-url docker.io/myorg --push
  arctl skill build ./my-skill --tag v1.0.0 --platform linux/amd64,linux/arm64`,
}

var (
	buildDockerURL  string
	buildTag        string
	buildPlatform   string
	buildPush       bool
	buildDockerName string
)

func init() {
	BuildCmd.Flags().StringVar(&buildDockerURL, "docker-url", "", "Docker registry URL prefix (e.g., docker.io/myorg)")
	BuildCmd.Flags().StringVar(&buildTag, "tag", "latest", "Docker image tag")
	BuildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
	BuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push Docker image after building")
	BuildCmd.Flags().StringVarP(&buildDockerName, "name", "n", "", "Override Docker image name (default: skill name from SKILL.md)")
}

func runSkillBuild(_ *cobra.Command, args []string) error {
	skillPath := args[0]

	// Validate the skill folder
	info, err := os.Stat(skillPath)
	if err != nil {
		return fmt.Errorf("skill path does not exist: %s", skillPath)
	}
	if !info.IsDir() {
		return fmt.Errorf("skill path is not a directory: %s", skillPath)
	}

	// Resolve skill name from SKILL.md frontmatter
	name, _, err := resolveSkillMeta(skillPath)
	if err != nil {
		return fmt.Errorf("failed to read skill metadata: %w", err)
	}

	if buildDockerName != "" {
		name = buildDockerName
	}

	// Construct image reference
	var imageRef string
	if buildDockerURL != "" {
		imageRef = common.BuildRegistryImageName(strings.TrimSuffix(buildDockerURL, "/"), name, buildTag)
	} else {
		imageRef = common.BuildLocalImageName(name, buildTag)
	}

	printer.PrintInfo(fmt.Sprintf("Building Docker image: %s", imageRef))

	// Build using inline Dockerfile (same approach as skill publish)
	buildArgs := []string{"build", "-t", imageRef}
	if buildPlatform != "" {
		buildArgs = append(buildArgs, "--platform", buildPlatform)
	}
	buildArgs = append(buildArgs, "-f", "-", skillPath)

	if verbose {
		printer.PrintInfo("Running: docker " + strings.Join(buildArgs, " "))
	}

	cmd := exec.Command("docker", buildArgs...)
	cmd.Dir = skillPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader("FROM scratch\nCOPY . .\n")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("Built Docker image: %s", imageRef))

	// Push if requested
	if buildPush {
		printer.PrintInfo(fmt.Sprintf("Pushing Docker image: %s", imageRef))
		executor := docker.NewExecutor(false, "")
		if err := executor.Push(imageRef); err != nil {
			return fmt.Errorf("docker push failed: %w", err)
		}
		printer.PrintSuccess(fmt.Sprintf("Pushed Docker image: %s", imageRef))
	}

	return nil
}
