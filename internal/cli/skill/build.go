package skill

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

This command reads the SKILL.md frontmatter to determine the skill name,
builds a Docker image, and optionally pushes it to a registry.

If the path contains multiple subdirectories with SKILL.md files, all will be built.`,
	Args:          cobra.ExactArgs(1),
	RunE:          runBuild,
	SilenceUsage:  true,
	SilenceErrors: false,
	Example: `  arctl skill build ./my-skill --image docker.io/myorg/my-skill:v1.0.0
  arctl skill build ./my-skill --image docker.io/myorg/my-skill:v1.0.0 --push
  arctl skill build ./my-skill --image docker.io/myorg/my-skill:v1.0.0 --platform linux/amd64`,
}

var (
	buildImage    string
	buildPush     bool
	buildPlatform string
)

func init() {
	BuildCmd.Flags().StringVar(&buildImage, "image", "", "Full image specification (e.g., docker.io/myorg/my-skill:v1.0.0)")
	BuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push the image to the registry")
	BuildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform for Docker build (e.g., linux/amd64, linux/arm64)")
}

func runBuild(cmd *cobra.Command, args []string) error {
	buildDir := args[0]
	if err := common.ValidateProjectDir(buildDir); err != nil {
		return err
	}

	absPath, err := filepath.Abs(buildDir)
	if err != nil {
		return fmt.Errorf("failed to resolve path %q: %w", buildDir, err)
	}

	skills, err := detectSkills(absPath)
	if err != nil {
		return fmt.Errorf("failed to detect skills: %w", err)
	}

	if len(skills) == 0 {
		return fmt.Errorf("no valid skills found at path: %s", absPath)
	}

	dockerExec := docker.NewExecutor(verbose, "")
	if err := dockerExec.CheckAvailability(); err != nil {
		return fmt.Errorf("docker check failed: %w", err)
	}

	for _, skillPath := range skills {
		if err := buildSkillImage(skillPath, dockerExec); err != nil {
			return err
		}
	}

	return nil
}

func buildSkillImage(skillPath string, dockerExec *docker.Executor) error {
	name, _, err := resolveSkillMeta(skillPath)
	if err != nil {
		return fmt.Errorf("failed to resolve skill metadata: %w", err)
	}

	imageName := buildImage
	if imageName == "" {
		return fmt.Errorf("--image is required (e.g., docker.io/myorg/%s:latest)", name)
	}

	printer.PrintInfo(fmt.Sprintf("Building skill %q as Docker image: %s", name, imageName))

	args := []string{"build", "-t", imageName}
	if buildPlatform != "" {
		args = append(args, "--platform", buildPlatform)
	}
	args = append(args, "-f", "-", skillPath)

	if verbose {
		printer.PrintInfo("Running: docker " + strings.Join(args, " "))
	}

	cmd := exec.Command("docker", args...)
	cmd.Dir = skillPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = strings.NewReader("FROM scratch\nCOPY . .\n")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed for skill %q: %w", name, err)
	}

	printer.PrintSuccess(fmt.Sprintf("Successfully built Docker image: %s", imageName))

	if buildPush {
		printer.PrintInfo(fmt.Sprintf("Pushing Docker image %s...", imageName))
		if err := dockerExec.Push(imageName); err != nil {
			return fmt.Errorf("docker push failed for skill %q: %w", name, err)
		}
	}

	return nil
}
