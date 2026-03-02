package skill

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var (
	// Flags for skill publish command
	dockerUrl        string
	tagFlag          string
	versionFlag      string
	platformFlag     string
	pushFlag         bool
	dryRunFlag       bool
	githubRepository string
	publishDesc      string
)

// githubRawBaseURL is the base URL for raw GitHub content checks.
// Exposed as a variable for testing.
var githubRawBaseURL = "https://raw.githubusercontent.com"

var PublishCmd = &cobra.Command{
	Use:   "publish <skill-name|skill-folder-path>",
	Short: "Publish a skill to the registry",
	Long: `Publish a skill to the agent registry.

This command supports two modes:

1. From a local skill folder (with SKILL.md):
   arctl skill publish ./my-skill --docker-url docker.io/myorg
   arctl skill publish ./my-skill --github https://github.com/org/repo --version 1.0.0

2. Direct registration (without a local SKILL.md, GitHub only):
   arctl skill publish my-skill \
     --github https://github.com/org/repo/tree/main/skills/my-skill \
     --version 1.0.0 \
     --description "My remote skill"

In both modes, SKILL.md must exist at the specified GitHub path.
In folder mode, the local skill folder must also contain a SKILL.md file with proper YAML frontmatter.
If the path contains multiple subdirectories with SKILL.md files, all will be published.`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

func init() {
	// Common flags
	PublishCmd.Flags().StringVar(&versionFlag, "version", "", "Version to publish (required for --github, optional override for --docker-url)")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")

	PublishCmd.Flags().StringVar(&publishDesc, "description", "", "Skill description (optional, used with direct registration)")
	PublishCmd.Flags().StringVar(&githubRepository, "github", "", "GitHub repository URL (alternative to --docker-url). Supports tree URLs: https://github.com/owner/repo/tree/branch/path")

	// Docker-only flags
	PublishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL. For example: docker.io/myorg. The final image name will be <docker-url>/<skill-name>:<tag>")
	PublishCmd.Flags().StringVar(&tagFlag, "tag", "latest", "Docker image tag (only used with --docker-url)")
	PublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Push image to Docker registry (only used with --docker-url)")
	PublishCmd.Flags().StringVar(&platformFlag, "platform", "", "Target platform for Docker build (only used with --docker-url, e.g. linux/amd64,linux/arm64)")

	PublishCmd.MarkFlagsMutuallyExclusive("docker-url", "github")
	PublishCmd.MarkFlagsOneRequired("docker-url", "github")
}

func runPublish(cmd *cobra.Command, args []string) error {
	input := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Detect whether input is a skill folder or a skill name.
	// If it's a directory that contains (or has subdirectories with) SKILL.md, use folder mode.
	// Otherwise, treat it as a skill name for direct registration.
	absPath, err := filepath.Abs(input)
	if err != nil {
		return fmt.Errorf("failed to resolve path %q: %w", input, err)
	}
	isFolder := false
	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		if _, detectErr := detectSkills(absPath); detectErr == nil {
			isFolder = true
		}
	}

	if isFolder {
		return runPublishFromFolder(absPath)
	}

	// Direct mode only supports GitHub. If --docker-url was specified but
	// the input isn't a folder with SKILL.md, give a targeted error.
	if dockerUrl != "" {
		return fmt.Errorf("--docker-url requires a local skill folder containing SKILL.md, but %q is not a valid skill folder", input)
	}

	return runPublishDirect(input)
}

// runPublishFromFolder publishes skills found in the given directory.
func runPublishFromFolder(absPath string) error {
	printer.PrintInfo(fmt.Sprintf("Publishing skill from: %s", absPath))

	skills, err := detectSkills(absPath)
	if err != nil {
		return fmt.Errorf("failed to detect skills: %w", err)
	}

	if len(skills) == 0 {
		return fmt.Errorf("no valid skills found at path: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Found %d skill(s) to publish", len(skills)))

	var errs []error

	for _, skill := range skills {
		printer.PrintInfo(fmt.Sprintf("Processing skill: %s", skill))

		var skillJson *models.SkillJSON
		switch {
		case githubRepository != "":
			skillJson, err = buildSkillFromGitHub(skill)
		case dockerUrl != "":
			skillJson, err = buildSkillDockerImage(skill)
		default:
			return fmt.Errorf("no build method specified")
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to build skill '%s': %w", skill, err))
			continue
		}

		if err := publishSkillJSON(skillJson); err != nil {
			errs = append(errs, err)
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

// runPublishDirect publishes a skill by name using --github and --version flags
// without requiring a local SKILL.md.
func runPublishDirect(skillName string) error {
	skillJson, err := buildSkillDirect(skillName)
	if err != nil {
		return err
	}

	if err := publishSkillJSON(skillJson); err != nil {
		return err
	}

	if !dryRunFlag {
		printer.PrintSuccess(fmt.Sprintf("Published: %s (v%s)", skillJson.Name, skillJson.Version))
	}

	return nil
}

// publishSkillJSON publishes or dry-runs a single SkillJSON.
func publishSkillJSON(skillJson *models.SkillJSON) error {
	if dryRunFlag {
		j, _ := json.Marshal(skillJson)
		printer.PrintInfo("[DRY RUN] Would publish skill to registry " + apiClient.BaseURL + ": " + string(j))
		return nil
	}

	_, err := apiClient.CreateSkill(skillJson)
	if err != nil {
		return fmt.Errorf("failed to publish skill '%s': %w", skillJson.Name, err)
	}
	return nil
}

// buildSkillDirect builds SkillJSON from command line flags without a local SKILL.md.
func buildSkillDirect(skillName string) (*models.SkillJSON, error) {
	skillName = strings.ToLower(skillName)

	if githubRepository == "" {
		return nil, fmt.Errorf("--github is required when publishing without SKILL.md")
	}
	if versionFlag == "" {
		return nil, fmt.Errorf("--version is required when publishing without SKILL.md")
	}

	if err := checkGitHubSkillMdExists(githubRepository); err != nil {
		return nil, fmt.Errorf("--github validation failed: %w", err)
	}

	return &models.SkillJSON{
		Name:        skillName,
		Description: publishDesc,
		Version:     versionFlag,
		Repository: &models.SkillRepository{
			URL:    githubRepository,
			Source: "github",
		},
	}, nil
}

type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// parseSkillFrontmatter reads and parses the YAML frontmatter from a SKILL.md file.
func parseSkillFrontmatter(skillPath string) (*skillFrontmatter, error) {
	skillMd := filepath.Join(skillPath, "SKILL.md")
	f, err := os.Open(skillMd)
	if err != nil {
		return nil, fmt.Errorf("failed to open SKILL.md: %w", err)
	}
	defer func() { _ = f.Close() }()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed reading SKILL.md: %w", err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("SKILL.md is empty")
	}

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
		return nil, fmt.Errorf("SKILL.md missing YAML frontmatter delimited by ---")
	}
	yamlContent := strings.Join(lines[yamlStart:yamlEnd], "\n")

	var fm skillFrontmatter
	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return nil, fmt.Errorf("failed to parse SKILL.md frontmatter: %w", err)
	}

	return &fm, nil
}

// resolveSkillMeta parses SKILL.md frontmatter and resolves the skill name.
func resolveSkillMeta(skillPath string) (name, description string, err error) {
	fm, err := parseSkillFrontmatter(skillPath)
	if err != nil {
		return "", "", err
	}

	name = fm.Name
	if name == "" {
		name = filepath.Base(skillPath)
	}

	return name, fm.Description, nil
}

// resolveDockerVersion returns the version for a Docker-based publish.
// Prefers --version if set, otherwise uses --tag (default "latest").
func resolveDockerVersion() string {
	if versionFlag != "" {
		return versionFlag
	}
	if tagFlag != "" {
		return tagFlag
	}
	return "latest"
}

// resolveGitHubVersion returns the version for a GitHub-based publish.
// Requires --version to be set.
func resolveGitHubVersion() (string, error) {
	if versionFlag == "" {
		return "", fmt.Errorf("--version is required when publishing with --github")
	}
	return versionFlag, nil
}

// checkGitHubSkillMdExists verifies that a SKILL.md file exists at the given
// GitHub repository URL by making an HTTP request to raw.githubusercontent.com.
func checkGitHubSkillMdExists(rawURL string) error {
	cloneURL, branch, subPath, err := parseGitHubURL(rawURL)
	if err != nil {
		return err
	}

	// Extract owner/repo from clone URL (https://github.com/{owner}/{repo}.git)
	cu, _ := url.Parse(cloneURL)
	cloneParts := strings.Split(strings.Trim(cu.Path, "/"), "/")
	owner := cloneParts[0]
	repo := strings.TrimSuffix(cloneParts[1], ".git")

	skillMdPath := "SKILL.md"
	if subPath != "" {
		skillMdPath = subPath + "/SKILL.md"
	}

	ref := branch
	if ref == "" {
		ref = "HEAD"
	}

	checkURL := fmt.Sprintf("%s/%s/%s/%s/%s", githubRawBaseURL, owner, repo, ref, skillMdPath)

	resp, err := http.Get(checkURL) //nolint:gosec // URL is constructed from validated GitHub components
	if err != nil {
		return fmt.Errorf("failed to verify SKILL.md at GitHub: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("SKILL.md not found at %s (ensure the file exists and the repository is public)", rawURL)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to verify SKILL.md at GitHub (HTTP %d)", resp.StatusCode)
	}

	return nil
}

func buildSkillDockerImage(skillPath string) (*models.SkillJSON, error) {
	name, description, err := resolveSkillMeta(skillPath)
	if err != nil {
		return nil, err
	}

	ver := resolveDockerVersion()

	// Determine image reference and build
	if dockerUrl == "" {
		return nil, fmt.Errorf("docker url is required")
	}

	// BuildRegistryImageName sanitizes the name for docker (lowercase, kebab-case)
	imageRef := common.BuildRegistryImageName(strings.TrimSuffix(dockerUrl, "/"), name, ver)
	// Build only if not dry-run
	if dryRunFlag {
		printer.PrintInfo("[DRY RUN] Would build Docker image: " + imageRef)
	} else {
		// Use classic docker build with Dockerfile provided via stdin (-f -)
		args := []string{"build", "-t", imageRef}

		// Add platform flag if specified
		if platformFlag != "" {
			args = append(args, "--platform", platformFlag)
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
			return nil, fmt.Errorf("docker build failed: %w", err)
		}
	}

	// Push tags if requested
	if pushFlag {
		if dryRunFlag {
			printer.PrintInfo("[DRY RUN] Would push Docker image: " + imageRef)
		} else {
			printer.PrintInfo("Pushing Docker image: docker push " + imageRef)
			pushCmd := exec.Command("docker", "push", imageRef)
			pushCmd.Stdout = os.Stdout
			pushCmd.Stderr = os.Stderr
			if err := pushCmd.Run(); err != nil {
				return nil, fmt.Errorf("docker push failed for %s: %w", imageRef, err)
			}
		}
	}

	// 3) Construct SkillJSON payload
	skill := &models.SkillJSON{
		Name:        name,
		Description: description,
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

	return skill, nil
}

// buildSkillFromGitHub reads SKILL.md frontmatter and registers the skill with a GitHub repository.
func buildSkillFromGitHub(skillPath string) (*models.SkillJSON, error) {
	name, description, err := resolveSkillMeta(skillPath)
	if err != nil {
		return nil, err
	}

	ver, err := resolveGitHubVersion()
	if err != nil {
		return nil, err
	}

	// Validate the GitHub URL and verify SKILL.md exists at the remote path
	if err := checkGitHubSkillMdExists(githubRepository); err != nil {
		return nil, fmt.Errorf("--github validation failed: %w", err)
	}

	skill := &models.SkillJSON{
		Name:        name,
		Description: description,
		Version:     ver,
		Repository: &models.SkillRepository{
			URL:    githubRepository,
			Source: "github",
		},
	}

	return skill, nil
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
