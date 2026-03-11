package skill

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var pullVersion string

var PullCmd = &cobra.Command{
	Use:   "pull <skill-name> [output-directory]",
	Short: "Pull a skill from the registry and extract it locally",
	Long: `Pull a skill from the registry and extract its contents to a local directory.
Supports skills packaged as Docker images or hosted in Git repositories.

If output-directory is not specified, it will be extracted to ./skills/<skill-name>`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPull,
}

func init() {
	PullCmd.Flags().StringVar(&pullVersion, "version", "", "Version to pull (if not specified and multiple versions exist, you will be prompted)")
}

func runPull(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Determine output directory
	outputDir := ""
	if len(args) > 1 {
		outputDir = args[1]
	} else {
		outputDir = filepath.Join("skills", skillName)
	}

	printer.PrintInfo(fmt.Sprintf("Pulling skill: %s", skillName))

	// 1. Resolve which version to pull
	version, err := resolveSkillVersion(skillName, pullVersion)
	if err != nil {
		return err
	}

	// 2. Fetch skill metadata from registry
	printer.PrintInfo("Fetching skill metadata from registry...")
	skillResp, err := apiClient.GetSkillByNameAndVersion(skillName, version)
	if err != nil {
		return fmt.Errorf("failed to fetch skill from registry: %w", err)
	}

	if skillResp == nil {
		return fmt.Errorf("skill '%s' version '%s' not found in registry", skillName, version)
	}

	printer.PrintSuccess(fmt.Sprintf("Found skill: %s (version %s)", skillResp.Skill.Name, skillResp.Skill.Version))

	// 2. Determine source: Docker package or git repository
	var dockerImage string
	for _, pkg := range skillResp.Skill.Packages {
		if pkg.RegistryType == "docker" {
			dockerImage = pkg.Identifier
			break
		}
	}

	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	if err := os.MkdirAll(absOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	if dockerImage != "" {
		if err := pullFromDocker(dockerImage, absOutputDir); err != nil {
			return err
		}
	} else if skillResp.Skill.Repository != nil && skillResp.Skill.Repository.Source == "git" {
		if err := pullFromGit(skillResp.Skill.Repository.URL, absOutputDir); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("skill has no Docker package or Git repository")
	}

	printer.PrintSuccess(fmt.Sprintf("Successfully pulled skill to: %s", absOutputDir))
	return nil
}

// resolveSkillVersion determines which version to pull.
// If a version is explicitly provided, it is used directly.
// If only one version exists, that version is selected automatically.
// If multiple versions exist, the user is prompted to specify one.
func resolveSkillVersion(skillName, requestedVersion string) (string, error) {
	if requestedVersion != "" {
		return requestedVersion, nil
	}

	versions, err := apiClient.GetSkillVersions(skillName)
	if err != nil {
		return "", fmt.Errorf("failed to list skill versions: %w", err)
	}

	if len(versions) == 0 {
		return "", fmt.Errorf("skill '%s' not found in registry", skillName)
	}

	if len(versions) == 1 {
		return versions[0].Skill.Version, nil
	}

	printer.PrintError(fmt.Sprintf("skill '%s' has %d versions, please specify one with --version:", skillName, len(versions)))
	for _, v := range versions {
		latest := ""
		if v.Meta.Official != nil && v.Meta.Official.IsLatest {
			latest = " (latest)"
		}
		printer.PrintInfo(fmt.Sprintf("  %s%s", v.Skill.Version, latest))
	}

	return "", fmt.Errorf("multiple versions available, specify one with --version")
}

// pullFromDocker pulls a skill from a Docker image and extracts its contents.
func pullFromDocker(dockerImage, absOutputDir string) error {
	printer.PrintInfo(fmt.Sprintf("Docker image: %s", dockerImage))

	printer.PrintInfo("Pulling Docker image...")
	pullCmd := exec.Command("docker", "pull", dockerImage)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("failed to pull Docker image: %w", err)
	}

	printer.PrintInfo(fmt.Sprintf("Extracting skill contents to: %s", absOutputDir))

	// Create a container from the image (without running it)
	createCmd := exec.Command("docker", "create", "--entrypoint", "/bin/sh", dockerImage, "-c", "echo")
	createOutput, err := createCmd.CombinedOutput()
	if err != nil {
		// If that fails, try without entrypoint override (for images with proper entrypoints)
		createCmd = exec.Command("docker", "create", dockerImage)
		createOutput, err = createCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to create container from image: %w\nOutput: %s", err, string(createOutput))
		}
	}
	containerIDStr := strings.TrimSpace(string(createOutput))

	defer func() {
		rmCmd := exec.Command("docker", "rm", containerIDStr)
		_ = rmCmd.Run()
	}()

	tempDir, err := os.MkdirTemp("", "skill-extract-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	cpCmd := exec.Command("docker", "cp", containerIDStr+":"+"/.", tempDir)
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("failed to extract contents from container: %w", err)
	}

	// Copy only non-empty files and folders to the final destination
	if err := docker.CopyNonEmptyContents(tempDir, absOutputDir); err != nil {
		return fmt.Errorf("failed to copy non-empty contents: %w", err)
	}

	return nil
}

// pullFromGit clones a git repository and copies the skill files to the output directory.
func pullFromGit(repoURL, absOutputDir string) error {
	printer.PrintInfo(fmt.Sprintf("Cloning from git: %s", repoURL))
	return gitutil.CloneAndCopy(repoURL, absOutputDir, true)
}
