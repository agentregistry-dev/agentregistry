package skill

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/config"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var (
	// Flags for skill push command
	pushDockerUrl string
	pushDockerTag string
	// Should push to docker registry
	pushDockerPushFlag bool
	pushPlatformFlag   string
	pushDryRunFlag     bool
)

var PushCmd = &cobra.Command{
	Use:   "push <skill-folder-path>",
	Short: "Wrap and push a Claude Skill as a Docker image with auto-approval",
	Long: `Wrap a Claude Skill in a Docker image, push it to both Docker registry and agent registry,
and automatically approve it.

The skill folder must contain a SKILL.md file with proper YAML frontmatter.
This command pushes the skill to the registry (unpublished) and then automatically approves it,
making it ready for publishing. This is useful for automated workflows where approval is automatic.`,
	Args: cobra.ExactArgs(1),
	RunE: runPush,
}

func init() {
	// Flags for push command
	PushCmd.Flags().StringVar(&pushDockerUrl, "docker-url", "", "Docker registry URL. For example: docker.io/myorg. The final image name will be <docker-url>/<skill-name>:<tag>")
	PushCmd.Flags().StringVar(&pushDockerTag, "tag", "latest", "Docker image tag to use")
	PushCmd.Flags().StringVar(&pushPlatformFlag, "platform", "", "Target platform(s) for the build (e.g., linux/amd64, linux/arm64, or linux/amd64,linux/arm64)")
	PushCmd.Flags().BoolVar(&pushDockerPushFlag, "push-docker", false, "Push to Docker registry")
	PushCmd.Flags().BoolVar(&pushDryRunFlag, "dry-run", false, "Show what would be done without actually doing it")

	_ = PushCmd.MarkFlagRequired("docker-url")
}

func runPush(cmd *cobra.Command, args []string) error {
	skillPath := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Detect skills
	skills, err := getSkills(skillPath)
	if err != nil {
		return fmt.Errorf("failed to get skills: %w", err)
	}

	var errs []error

	for _, skill := range skills {
		printer.PrintInfo(fmt.Sprintf("Processing skill: %s", skill))

		imageRef, skillJson, err := buildSkillDockerImage(skill, pushDryRunFlag)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to build skill '%s': %w", skill, err))
			continue
		}

		// Push Docker image
		if pushDockerPushFlag {
			if err := pushSkillDockerImage(imageRef, pushDryRunFlag); err != nil {
				errs = append(errs, fmt.Errorf("failed to push docker image: %w", err))
				continue
			}
		}

		if pushDryRunFlag {
			j, _ := json.Marshal(skillJson)
			printer.PrintInfo("[DRY RUN] Would push skill to registry " + apiClient.BaseURL + ": " + string(j))
		} else {
			// Push to registry (unpublished)
			_, err = apiClient.PushSkill(skillJson)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to push skill '%s': %w", skill, err))
				continue
			}

			// Auto-approve the skill (if configured)
			if config.GetAutoApprove() {
				if err := apiClient.ApproveSkillStatus(skillJson.Name, skillJson.Version, "Auto-approved via push command"); err != nil {
					errs = append(errs, fmt.Errorf("failed to approve skill '%s': %w", skill, err))
					continue
				}
			}
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("one or more errors occurred during pushing: %w", errors.Join(errs...))
	}

	printer.PrintSuccess("Skill pushing and approval complete!")

	return nil
}

func getSkills(skillPath string) ([]string, error) {
	// Validate path exists
	absPath, err := filepath.Abs(skillPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve skill path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("skill path does not exist: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Pushing skill from: %s", absPath))

	// Detect skills
	skills, err := detectSkills(absPath)
	if err != nil {
		return nil, fmt.Errorf("failed to detect skills: %w", err)
	}

	if len(skills) == 0 {
		return nil, fmt.Errorf("no valid skills found at path: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Found %d skill(s) to push", len(skills)))

	return skills, nil
}

func buildSkillDockerImage(skillPath string, dryRun bool) (string, *models.SkillJSON, error) {
	// 1) Read and parse SKILL.md frontmatter
	skillMd := filepath.Join(skillPath, "SKILL.md")
	f, err := os.Open(skillMd)
	if err != nil {
		return "", nil, fmt.Errorf("failed to open SKILL.md: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Extract YAML frontmatter between leading --- blocks
	type frontmatter struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return "", nil, fmt.Errorf("failed reading SKILL.md: %w", err)
	}
	if len(lines) == 0 {
		return "", nil, fmt.Errorf("SKILL.md is empty")
	}

	// Find frontmatter region
	var yamlStart, yamlEnd = -1, -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "---" {
			if yamlStart == -1 {
				yamlStart = i + 1
			} else {
				yamlEnd = i
				break
			}
		}
	}
	if yamlStart == -1 || yamlEnd == -1 || yamlEnd <= yamlStart {
		return "", nil, fmt.Errorf("SKILL.md missing YAML frontmatter delimited by ---")
	}
	yamlContent := strings.Join(lines[yamlStart:yamlEnd], "\n")

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return "", nil, fmt.Errorf("failed to parse SKILL.md frontmatter: %w", err)
	}

	// Defaults and overrides
	if fm.Name == "" {
		// fallback to directory name
		fm.Name = filepath.Base(skillPath)
	}
	ver := pushDockerTag
	if ver == "" {
		ver = "latest"
	}

	// 2) Determine image reference and build
	// sanitize name for docker (lowercase, slashes to dashes)
	repoName := sanitizeRepoName(fm.Name)
	if pushDockerUrl == "" {
		return "", nil, fmt.Errorf("docker url is required")
	}

	imageRef := fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(pushDockerUrl, "/"), repoName, ver)

	if dryRun {
		printer.PrintInfo("[DRY RUN] Would build Docker image: " + imageRef)
	} else {
		// Build Docker image
		args := []string{"build", "-t", imageRef}

		// Add platform flag if specified
		if pushPlatformFlag != "" {
			args = append(args, "--platform", pushPlatformFlag)
		}

		args = append(args, "-f", "-", skillPath)

		printer.PrintInfo("Building Docker image (Dockerfile via stdin): docker " + strings.Join(args, " "))
		cmd := exec.Command("docker", args...)
		cmd.Dir = skillPath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		// Minimal inline Dockerfile; avoids requiring a Dockerfile in the skill folder
		cmd.Stdin = strings.NewReader("FROM scratch\nCOPY . .\n")
		if err := cmd.Run(); err != nil {
			return "", nil, fmt.Errorf("docker build failed: %w", err)
		}
	}

	// 3) Construct SkillJSON payload
	skill := &models.SkillJSON{
		Name:        fm.Name,
		Description: fm.Description,
		Version:     ver,
	}

	// package info for docker image
	pkg := models.SkillPackageInfo{
		RegistryType: "docker",
		Identifier:   imageRef,
		Version:      ver,
	}
	pkg.Transport.Type = "docker"
	skill.Packages = append(skill.Packages, pkg)

	return imageRef, skill, nil
}

func pushSkillDockerImage(imageRef string, dryRun bool) error {
	if dryRun {
		printer.PrintInfo("[DRY RUN] Would push Docker image: " + imageRef)
		return nil
	}

	printer.PrintInfo("Pushing Docker image: docker push " + imageRef)
	pushCmd := exec.Command("docker", "push", imageRef)
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("docker push failed for %s: %w", imageRef, err)
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
