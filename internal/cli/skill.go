package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/skill"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var (
	// Flags for skill publish command
	dockerUrl    string
	dockerTag    string
	platformFlag string
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

var skillPullCmd = &cobra.Command{
	Use:   "pull <skill-name> [output-directory]",
	Short: "Pull a skill from the registry and extract it locally",
	Long: `Pull a skill's Docker image from the registry and extract its contents to a local directory.
	
If output-directory is not specified, it will be extracted to ./skills/<skill-name>`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runSkillPull,
}

func runSkillPull(cmd *cobra.Command, args []string) error {
	skillName := args[0]

	// Determine output directory
	outputDir := ""
	if len(args) > 1 {
		outputDir = args[1]
	} else {
		outputDir = filepath.Join("skills", sanitizeRepoName(skillName))
	}

	printer.PrintInfo(fmt.Sprintf("Pulling skill: %s", skillName))

	// 1. Fetch skill metadata from registry
	printer.PrintInfo("Fetching skill metadata from registry...")
	skill, err := APIClient.GetSkillByName(skillName)
	if err != nil {
		return fmt.Errorf("failed to fetch skill from registry: %w", err)
	}

	if skill == nil {
		return fmt.Errorf("skill '%s' not found in registry", skillName)
	}

	printer.PrintSuccess(fmt.Sprintf("Found skill: %s (version %s)", skill.Skill.Name, skill.Skill.Version))

	// 2. Find Docker package in skill
	var dockerImage string
	for _, pkg := range skill.Skill.Packages {
		if pkg.RegistryType == "docker" {
			dockerImage = pkg.Identifier
			break
		}
	}

	if dockerImage == "" {
		return fmt.Errorf("skill does not have a Docker package")
	}

	printer.PrintInfo(fmt.Sprintf("Docker image: %s", dockerImage))

	// 3. Pull the Docker image
	printer.PrintInfo("Pulling Docker image...")
	pullCmd := exec.Command("docker", "pull", dockerImage)
	pullCmd.Stdout = os.Stdout
	pullCmd.Stderr = os.Stderr
	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("failed to pull Docker image: %w", err)
	}

	// 4. Create output directory
	absOutputDir, err := filepath.Abs(outputDir)
	if err != nil {
		return fmt.Errorf("failed to resolve output directory: %w", err)
	}

	if err := os.MkdirAll(absOutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// 5. Extract contents from Docker image
	printer.PrintInfo(fmt.Sprintf("Extracting skill contents to: %s", absOutputDir))

	// For images built FROM scratch, we need to provide a dummy command
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

	// Ensure we clean up the container
	defer func() {
		rmCmd := exec.Command("docker", "rm", containerIDStr)
		_ = rmCmd.Run()
	}()

	// Extract to a temporary directory first
	tempDir, err := os.MkdirTemp("", "skill-extract-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	// Copy contents from container to temp directory
	cpCmd := exec.Command("docker", "cp", containerIDStr+":"+"/.", tempDir)
	cpCmd.Stderr = os.Stderr
	if err := cpCmd.Run(); err != nil {
		return fmt.Errorf("failed to extract contents from container: %w", err)
	}

	// Copy only non-empty files and folders to the final destination
	if err := copyNonEmptyContents(tempDir, absOutputDir); err != nil {
		return fmt.Errorf("failed to copy non-empty contents: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("Successfully pulled skill to: %s", absOutputDir))
	return nil
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

// copyNonEmptyContents recursively copies only non-empty files and directories
func copyNonEmptyContents(src, dst string) error {
	// Skip system directories that Docker creates
	skipDirs := map[string]bool{
		"dev":  true,
		"etc":  true,
		"proc": true,
		"sys":  true,
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		// Skip system directories at root level
		if src == filepath.Dir(srcPath) && skipDirs[entry.Name()] {
			continue
		}

		// Skip hidden Docker files
		if entry.Name() == ".dockerenv" {
			continue
		}

		if entry.IsDir() {
			// Check if directory has any non-empty content
			if !hasNonEmptyContent(srcPath, skipDirs) {
				continue
			}

			// Create directory in destination
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return fmt.Errorf("failed to create directory %s: %w", dstPath, err)
			}

			// Recursively copy contents
			if err := copyNonEmptyContents(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			// Check if file is non-empty
			info, err := os.Stat(srcPath)
			if err != nil {
				return fmt.Errorf("failed to stat file %s: %w", srcPath, err)
			}

			if info.Size() == 0 {
				continue
			}

			// Copy file
			if err := copyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy file %s: %w", srcPath, err)
			}
		}
	}

	return nil
}

// hasNonEmptyContent checks if a directory contains any non-empty files
func hasNonEmptyContent(dir string, skipDirs map[string]bool) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}

	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		if entry.IsDir() {
			if skipDirs[entry.Name()] {
				continue
			}
			if hasNonEmptyContent(path, skipDirs) {
				return true
			}
		} else {
			if entry.Name() == ".dockerenv" {
				continue
			}
			info, err := entry.Info()
			if err == nil && info.Size() > 0 {
				return true
			}
		}
	}

	return false
}

// copyFile copies a single file from src to dst
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = sourceFile.Close() }()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = destFile.Close() }()

	if _, err := io.Copy(destFile, sourceFile); err != nil {
		return err
	}

	// Copy permissions
	sourceInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	return os.Chmod(dst, sourceInfo.Mode())
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
	skillCmd.AddCommand(skillPullCmd)
	skillCmd.AddCommand(skill.InitCmd)

	// Flags for publish command
	skillPublishCmd.Flags().StringVar(&dockerUrl, "docker-url", "", "Docker registry URL. For example: docker.io/myorg. The final image name will be <docker-url>/<skill-name>:<tag>")
	skillPublishCmd.Flags().BoolVar(&pushFlag, "push", false, "Automatically push to Docker and agent registries")
	skillPublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
	skillPublishCmd.Flags().StringVar(&dockerTag, "tag", "latest", "Docker image tag to use")
	skillPublishCmd.Flags().StringVar(&platformFlag, "platform", "", "Target platform(s) for the build (e.g., linux/amd64, linux/arm64, or linux/amd64,linux/arm64)")

	_ = skillPublishCmd.MarkFlagRequired("docker-url")

	// Add skill command to root
	rootCmd.AddCommand(skillCmd)
}
