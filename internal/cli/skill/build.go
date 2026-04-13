package skill

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/annotations"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

// skillDockerfile is the minimal Dockerfile used for skill images.
const skillDockerfile = "FROM scratch\nCOPY . .\n"

var BuildCmd = &cobra.Command{
	Use:         "build <skill-folder-path>",
	Annotations: map[string]string{annotations.SkipDaemonAnnotation: "true"},
	Short:       "Build a skill as a Docker image",
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
	BuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push the image to the container registry, specififed by --image")
	BuildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform for Docker build (e.g., linux/amd64, linux/arm64)")

	BuildCmd.MarkFlagRequired("image")
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

	isValid := isValidSkillDir(absPath)

	if !isValid {
		return fmt.Errorf("no valid skills found at path: %s", absPath)
	}

	dockerExec := docker.NewExecutor(verbose, "")
	if err := dockerExec.CheckAvailability(); err != nil {
		return fmt.Errorf("docker check failed: %w", err)
	}

	if err := buildSkillImage(absPath, dockerExec); err != nil {
		return err
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

	// Write the inline Dockerfile to a temp file so we can use the standard Build method.
	tmpFile, err := os.CreateTemp("", "skill-dockerfile-*")
	if err != nil {
		return fmt.Errorf("failed to create temp Dockerfile: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(skillDockerfile); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp Dockerfile: %w", err)
	}
	tmpFile.Close()

	var extraArgs []string
	extraArgs = append(extraArgs, "-f", tmpFile.Name())
	if buildPlatform != "" {
		extraArgs = append(extraArgs, "--platform", buildPlatform)
	}

	exec := docker.NewExecutor(verbose, skillPath)
	if err := exec.Build(imageName, skillPath, extraArgs...); err != nil {
		return fmt.Errorf("build failed for skill %q: %w", name, err)
	}

	if buildPush {
		printer.PrintInfo(fmt.Sprintf("Pushing Docker image %s...", imageName))
		if err := dockerExec.Push(imageName); err != nil {
			return fmt.Errorf("docker push failed for skill %q: %w", name, err)
		}
	}

	return nil
}
