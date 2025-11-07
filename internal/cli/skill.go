package cli

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
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var (
	// Flags for skill publish command
	dockerUrl    string
	dockerTag    string
	registryName string
	pushFlag     bool
	dryRunFlag   bool
)

var skillCmd = &cobra.Command{
	Use:   "skill",
	Short: "Manage Claude Skills",
	Long:  `Wrap, publish, and manage Claude Skills as Docker images in the registry.`,
}

var skillPublishCmd = &cobra.Command{
	Use:   "publish <skill-folder-path>",
	Short: "Wrap and publish a Claude Skill as a Docker image",
	Long: `Wrap a Claude Skill in a Docker image and publish it to both Docker registry and agent registry.
	
The skill folder must contain a SKILL.md file with proper YAML frontmatter.
Use --multi flag to auto-detect and process multiple skill folders.`,
	Args: cobra.ExactArgs(1),
	RunE: runSkillPublish,
}

var skillListCmd = &cobra.Command{
	Use:   "list",
	Short: "List Claude Skills from connected registries",
	Long:  `List all Claude Skills available from connected registries.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runSkillList(cmd, args); err != nil {
			printer.PrintError(fmt.Sprintf("Failed to list skills: %v", err))
			os.Exit(1)
		}
	},
}

var skillShowCmd = &cobra.Command{
	Use:   "show <skill-name>",
	Short: "Show details of a Claude Skill",
	Long:  `Display detailed information about a specific Claude Skill.`,
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		if err := runSkillShow(cmd, args); err != nil {
			printer.PrintError(fmt.Sprintf("Failed to show skill: %v", err))
			os.Exit(1)
		}
	},
}

func runSkillPublish(cmd *cobra.Command, args []string) error {
	skillPath := args[0]

	// Validate path exists
	absPath, err := filepath.Abs(skillPath)
	if err != nil {
		return fmt.Errorf("failed to resolve skill path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return fmt.Errorf("skill path does not exist: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Publishing skill from: %s", absPath))

	// Detect skills
	skills, err := detectSkills(absPath)
	if err != nil {
		return fmt.Errorf("failed to detect skills: %w", err)
	}

	if len(skills) == 0 {
		return fmt.Errorf("no valid skills found at path: %s", absPath)
	}

	printer.PrintInfo(fmt.Sprintf("Found %d skill(s) to publish", len(skills)))

	// TODO: Implement the actual publishing logic
	// For each skill:
	// 1. Validate skill structure
	// 2. Build Docker image
	// 3. Push to Docker registry (if --push flag)
	// 4. Publish to agent registry

	var errs []error

	for _, skill := range skills {
		printer.PrintInfo(fmt.Sprintf("Processing skill: %s", skill))
		skillJson, err := buildSkillDockerImage(skill)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to build skill '%s': %w", skill, err))
			continue
		}

		if dryRunFlag {
			j, _ := json.Marshal(skillJson)
			printer.PrintInfo("[DRY RUN] Would publish skill to registry " + APIClient.BaseURL + ": " + string(j))
		} else {
			_, err = APIClient.PublishSkill(skillJson)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to publish skill '%s': %w", skill, err))
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

func buildSkillDockerImage(skillPath string) (*models.SkillJSON, error) {
	// 1) Read and parse SKILL.md frontmatter
	skillMd := filepath.Join(skillPath, "SKILL.md")
	f, err := os.Open(skillMd)
	if err != nil {
		return nil, fmt.Errorf("failed to open SKILL.md: %w", err)
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
		return nil, fmt.Errorf("failed reading SKILL.md: %w", err)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("SKILL.md is empty")
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
		return nil, fmt.Errorf("SKILL.md missing YAML frontmatter delimited by ---")
	}
	yamlContent := strings.Join(lines[yamlStart:yamlEnd], "\n")

	var fm frontmatter
	if err := yaml.Unmarshal([]byte(yamlContent), &fm); err != nil {
		return nil, fmt.Errorf("failed to parse SKILL.md frontmatter: %w", err)
	}

	// Defaults and overrides
	if fm.Name == "" {
		// fallback to directory name
		fm.Name = filepath.Base(skillPath)
	}
	ver := dockerTag
	if ver == "" {
		ver = "latest"
	}

	// 2) Determine image reference and build
	// sanitize name for docker (lowercase, slashes to dashes)
	repoName := sanitizeRepoName(fm.Name)
	if dockerUrl == "" {
		return nil, fmt.Errorf("docker url is required")
	}
	// dockerRegistry may be like docker.io or ghcr.io

	imageRef := fmt.Sprintf("%s/%s:%s", strings.TrimSuffix(dockerUrl, "/"), repoName, ver)
	// Build only if not dry-run
	if dryRunFlag {
		printer.PrintInfo("[DRY RUN] Would build Docker image: " + imageRef)
	} else {
		// Use classic docker build with Dockerfile provided via stdin (-f -)
		args := []string{"build", "-t", imageRef, "-f", "-", skillPath}

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
		Name: fm.Name,
		//		Title:       fm.Title,
		Description: fm.Description,
		Version:     ver,
		//WebsiteURL:  fm.WebsiteURL,
		//Repository: models.SkillRepository{
		//	URL:    fm.Repository.URL,
		//	Source: fm.Repository.Source,
		//},
	}

	// package info for docker image
	pkg := models.SkillPackageInfo{
		RegistryType: "docker",
		Identifier:   imageRef,
		Version:      ver,
	}
	pkg.Transport.Type = "docker"
	skill.Packages = append(skill.Packages, pkg)

	// remotes (optional)
	//for _, r := range fm.Remotes {
	//	if strings.TrimSpace(r.URL) == "" {
	//		continue
	//	}
	//	skill.Remotes = append(skill.Remotes, models.SkillRemoteInfo{URL: r.URL})
	//}

	return skill, nil
}

// sanitizeRepoName converts a skill name to a docker-friendly repo name
func sanitizeRepoName(name string) string {
	n := strings.TrimSpace(strings.ToLower(name))
	// replace any non-alphanum or separator with dash
	// also convert path separators to dashes
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")
	n = replacer.Replace(n)
	// collapse consecutive dashes
	for strings.Contains(n, "--") {
		n = strings.ReplaceAll(n, "--", "-")
	}
	n = strings.Trim(n, "-")
	if n == "" {
		n = "skill"
	}
	return n
}

func runSkillList(cmd *cobra.Command, args []string) error {
	if APIClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	skills, err := APIClient.GetSkills()
	if err != nil {
		return fmt.Errorf("failed to get skills: %w", err)
	}

	if len(skills) == 0 {
		printer.PrintInfo("No skills found. Connect to a registry or publish a skill.")
		return nil
	}

	// Create table printer
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Name", "Title", "Version", "Category", "Registry", "Installed")

	for _, skill := range skills {
		installedStatus := "No"
		if skill.Installed {
			installedStatus = "Yes"
		}

		title := skill.Title
		if title == "" {
			title = "-"
		}

		category := skill.Category
		if category == "" {
			category = "-"
		}

		t.AddRow(
			skill.Name,
			title,
			skill.Version,
			category,
			skill.RegistryName,
			installedStatus,
		)
	}

	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	return nil
}

func runSkillShow(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	if APIClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	skill, err := APIClient.GetSkillByName(skillName)
	if err != nil {
		return fmt.Errorf("failed to get skill: %w", err)
	}

	if skill == nil {
		return fmt.Errorf("skill '%s' not found", skillName)
	}

	// Display skill details in table format
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")

	t.AddRow("Name", skill.Name)
	t.AddRow("Title", printer.EmptyValueOrDefault(skill.Title, "<none>"))
	t.AddRow("Description", skill.Description)
	t.AddRow("Version", skill.Version)
	t.AddRow("Category", printer.EmptyValueOrDefault(skill.Category, "<none>"))
	t.AddRow("Registry", skill.RegistryName)

	installedStatus := "No"
	if skill.Installed {
		installedStatus = "Yes"
	}
	t.AddRow("Installed", installedStatus)

	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	// Print raw data if available
	if skill.Data != "" {
		fmt.Println("\nRaw Data:")
		fmt.Println(skill.Data)
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

func init() {
	// Add subcommands to skill command
	skillCmd.AddCommand(skillPublishCmd)
	skillCmd.AddCommand(skillListCmd)
	skillCmd.AddCommand(skillShowCmd)

	// Flags for publish command
	skillPublishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL. For example: docker.io/myorg. The final image name will be <docker-url>/<skill-name>:<tag>")
	skillPublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries")
	skillPublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	skillPublishCmd.Flags().StringVar(&dockerTag, "tag", "latest", "Docker image tag to use")

	_ = skillPublishCmd.MarkFlagRequired("docker-url")

	// Flags for list command
	skillListCmd.Flags().StringVar(&registryName, "registry", "", "Filter by registry name")

	// Add skill command to root
	rootCmd.AddCommand(skillCmd)
}
