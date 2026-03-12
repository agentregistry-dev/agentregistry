package skill

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/gitutil"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var (
	// Flags for skill publish command
	versionFlag      string
	dryRunFlag       bool
	gitRepository   string
	dockerImageFlag string
	publishDesc      string
)

// githubRawBaseURL is the base URL for raw GitHub content checks.
// Exposed as a variable for testing.
var githubRawBaseURL = "https://raw.githubusercontent.com"

var PublishCmd = &cobra.Command{
	Use:   "publish <skill-name|skill-folder-path>",
	Short: "Publish a skill to the registry",
	Long: `Publish a skill to the agent registry.

This command supports three modes:

1. From a local skill folder (with SKILL.md):
   arctl skill publish ./my-skill --git https://github.com/org/repo --version 1.0.0
   arctl skill publish ./my-skill --docker-image docker.io/myorg/my-skill:v1.0.0 --version 1.0.0

2. Direct registration with Git repository:
   arctl skill publish my-skill \
     --git https://github.com/org/repo/tree/main/skills/my-skill \
     --version 1.0.0 \
     --description "My remote skill"

3. Direct registration with a pre-built Docker image:
   arctl skill publish my-skill \
     --docker-image docker.io/myorg/my-skill:v1.0.0 \
     --version 1.0.0 \
     --description "My Docker skill"

For Git modes, SKILL.md must exist at the specified Git repository path.
In folder mode, the local skill folder must also contain a SKILL.md file with proper YAML frontmatter.

To build a skill as a Docker image, use "arctl skill build" instead.`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

func init() {
	// Common flags
	PublishCmd.Flags().StringVar(&versionFlag, "version", "", "Version to publish (required for --git or --docker-image)")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	PublishCmd.Flags().StringVar(&publishDesc, "description", "", "Skill description (optional, used with direct registration)")
	PublishCmd.Flags().StringVar(&gitRepository, "git", "", "Git repository URL (alternative to --docker-image). Supports tree URLs: https://github.com/owner/repo/tree/branch/path")

	// Docker-only flags
	PublishCmd.Flags().StringVar(&dockerImageFlag, "docker-image", "", "Docker image URL. For example: docker.io/myorg/my-skill:v1.0.0")

	PublishCmd.MarkFlagsOneRequired("git", "docker-image")
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
	if info, err := os.Stat(absPath); err == nil && info.IsDir() {
		isValid := isValidSkillDir(absPath)
		if !isValid {
			return fmt.Errorf("no valid skills found at path: %s", absPath)
		}
		return runPublishFromFolder(absPath)
	}

	return runPublishDirect(input)
}

// runPublishFromFolder publishes the pre-detected skills from the given directory.
func runPublishFromFolder(skillFolderPath string) error {
	printer.PrintInfo(fmt.Sprintf("Publishing skill from: %s", skillFolderPath))

	var skillJson *models.SkillJSON
	var err error
	switch {
	case gitRepository != "":
		skillJson, err = buildSkillFromGitHub(skillFolderPath)
	case dockerImageFlag != "":
		skillJson, err = buildSkillFromDocker(skillFolderPath)
	default:
		return fmt.Errorf("--git or --docker-image is required")
	}
	if err != nil {
		return fmt.Errorf("failed to build skill '%s': %w", skillFolderPath, err)
	}

	if err := publishSkillJSON(skillJson); err != nil {
		return err
	}

	return nil
}

// runPublishDirect publishes a skill by name using --git or --docker-image flags
// without requiring a local SKILL.md.
func runPublishDirect(skillName string) error {
	var skillJson *models.SkillJSON
	var err error

	switch {
	case gitRepository != "":
		skillJson, err = buildSkillDirectGitHub(skillName)
	case dockerImageFlag != "":
		skillJson, err = buildSkillDirectDocker(skillName)
	default:
		return fmt.Errorf("--git or --docker-image is required")
	}
	if err != nil {
		return err
	}

	if err := publishSkillJSON(skillJson); err != nil {
		return err
	}

	if !dryRunFlag {
		printer.PrintSuccess(fmt.Sprintf("Published: %s (%s)", skillJson.Name, common.FormatVersionForDisplay(skillJson.Version)))
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

// buildSkillDirectGitHub builds SkillJSON from --git flags without a local SKILL.md.
func buildSkillDirectGitHub(skillName string) (*models.SkillJSON, error) {
	skillName = strings.ToLower(skillName)

	if gitRepository == "" {
		return nil, fmt.Errorf("--git is required when publishing without SKILL.md")
	}
	if versionFlag == "" {
		return nil, fmt.Errorf("--version is required when publishing without SKILL.md")
	}

	if err := checkGitHubSkillMdExists(gitRepository); err != nil {
		return nil, fmt.Errorf("--git validation failed: %w", err)
	}

	return &models.SkillJSON{
		Name:        skillName,
		Description: publishDesc,
		Version:     versionFlag,
		Repository: &models.SkillRepository{
			URL:    gitRepository,
			Source: "git",
		},
	}, nil
}

// buildSkillDirectDocker builds SkillJSON from --docker-image flags without a local SKILL.md.
func buildSkillDirectDocker(skillName string) (*models.SkillJSON, error) {
	skillName = strings.ToLower(skillName)

	if dockerImageFlag == "" {
		return nil, fmt.Errorf("--docker-image is required")
	}
	if versionFlag == "" {
		return nil, fmt.Errorf("--version is required when publishing with --docker-image")
	}

	skill := &models.SkillJSON{
		Name:        skillName,
		Description: publishDesc,
		Version:     versionFlag,
	}

	pkg := models.SkillPackageInfo{
		RegistryType: "docker",
		Identifier:   dockerImageFlag,
		Version:      versionFlag,
	}
	pkg.Transport.Type = "docker"
	skill.Packages = append(skill.Packages, pkg)

	return skill, nil
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

	if fm.Name == "" {
		return nil, fmt.Errorf("SKILL.md frontmatter missing required field: name")
	}
	if fm.Description == "" {
		return nil, fmt.Errorf("SKILL.md frontmatter missing required field: description")
	}

	return &fm, nil
}

// resolveSkillMeta parses SKILL.md frontmatter and returns the skill name and description.
func resolveSkillMeta(skillPath string) (name, description string, err error) {
	fm, err := parseSkillFrontmatter(skillPath)
	if err != nil {
		return "", "", err
	}
	return fm.Name, fm.Description, nil
}

// resolveGitHubVersion returns the version for a GitHub-based publish.
// Requires --version to be set.
func resolveGitHubVersion() (string, error) {
	if versionFlag == "" {
		return "", fmt.Errorf("--version is required when publishing with --git")
	}
	return versionFlag, nil
}

// checkGitHubSkillMdExists verifies that a SKILL.md file exists at the given
// GitHub repository URL by making an HTTP request to raw.githubusercontent.com.
func checkGitHubSkillMdExists(rawURL string) error {
	cloneURL, branch, subPath, err := gitutil.ParseGitHubURL(rawURL)
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

	// Validate the Git URL and verify SKILL.md exists at the remote path
	if err := checkGitHubSkillMdExists(gitRepository); err != nil {
		return nil, fmt.Errorf("--git validation failed: %w", err)
	}

	skill := &models.SkillJSON{
		Name:        name,
		Description: description,
		Version:     ver,
		Repository: &models.SkillRepository{
			URL:    gitRepository,
			Source: "git",
		},
	}

	return skill, nil
}

// buildSkillFromDocker reads SKILL.md frontmatter and registers the skill with a Docker image reference.
func buildSkillFromDocker(skillPath string) (*models.SkillJSON, error) {
	name, description, err := resolveSkillMeta(skillPath)
	if err != nil {
		return nil, err
	}

	if versionFlag == "" {
		return nil, fmt.Errorf("--version is required when publishing with --docker-image")
	}

	skill := &models.SkillJSON{
		Name:        name,
		Description: description,
		Version:     versionFlag,
	}

	pkg := models.SkillPackageInfo{
		RegistryType: "docker",
		Identifier:   dockerImageFlag,
		Version:      versionFlag,
	}
	pkg.Transport.Type = "docker"
	skill.Packages = append(skill.Packages, pkg)

	return skill, nil
}

// isValidSkillDir checks whether a directory contains a SKILL.md with valid YAML frontmatter.
func isValidSkillDir(dir string) bool {
	if !hasSkillMd(dir) {
		return false
	}
	_, err := parseSkillFrontmatter(dir)
	return err == nil
}

// hasSkillMd checks whether a directory contains a SKILL.md file.
func hasSkillMd(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "SKILL.md"))
	return err == nil
}
