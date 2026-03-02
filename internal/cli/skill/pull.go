package skill

import (
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var pullVersion string

var PullCmd = &cobra.Command{
	Use:   "pull <skill-name> [output-directory]",
	Short: "Pull a skill from the registry and extract it locally",
	Long: `Pull a skill from the registry and extract its contents to a local directory.
Supports skills packaged as Docker images or hosted in GitHub repositories.

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

	// 2. Determine source: Docker package or GitHub repository
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
	} else if skillResp.Skill.Repository != nil && skillResp.Skill.Repository.Source == "github" {
		if err := pullFromGitHub(skillResp.Skill.Repository.URL, absOutputDir); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("skill has no Docker package or GitHub repository")
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

// pullFromGitHub clones a GitHub repository and copies the skill files to the output directory.
func pullFromGitHub(repoURL, absOutputDir string) error {
	cloneURL, branch, subPath, err := parseGitHubURL(repoURL)
	if err != nil {
		return fmt.Errorf("failed to parse GitHub URL: %w", err)
	}

	printer.PrintInfo(fmt.Sprintf("Cloning from GitHub: %s", cloneURL))

	tempDir, err := os.MkdirTemp("", "skill-github-*")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }()

	cloneArgs := []string{"clone", "--depth", "1"}
	if branch != "" {
		cloneArgs = append(cloneArgs, "--branch", branch)
	}
	cloneArgs = append(cloneArgs, cloneURL, tempDir)

	gitCmd := exec.Command("git", cloneArgs...)
	gitCmd.Stdout = os.Stdout
	gitCmd.Stderr = os.Stderr
	if err := gitCmd.Run(); err != nil {
		return fmt.Errorf("failed to clone GitHub repository: %w", err)
	}

	return copyRepoContents(tempDir, subPath, absOutputDir)
}

// copyRepoContents copies files from a cloned repository to the output directory.
// It navigates to the subPath if specified and skips the .git directory.
// Symlinks are skipped to prevent symlink traversal attacks from untrusted repos.
func copyRepoContents(repoDir, subPath, absOutputDir string) error {
	srcDir := repoDir
	if subPath != "" {
		srcDir = filepath.Join(repoDir, subPath)
		if _, err := os.Stat(srcDir); os.IsNotExist(err) {
			return fmt.Errorf("subdirectory %q not found in repository", subPath)
		}
	}

	printer.PrintInfo(fmt.Sprintf("Copying skill contents to: %s", absOutputDir))

	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("failed to read source directory: %w", err)
	}

	for _, entry := range entries {
		// Skip .git directory
		if entry.Name() == ".git" {
			continue
		}

		// Skip symlinks to prevent traversal attacks from untrusted repos
		if entry.Type()&os.ModeSymlink != 0 {
			printer.PrintInfo(fmt.Sprintf("Skipping symlink: %s", entry.Name()))
			continue
		}

		srcPath := filepath.Join(srcDir, entry.Name())
		dstPath := filepath.Join(absOutputDir, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy directory %s: %w", entry.Name(), err)
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return fmt.Errorf("failed to copy file %s: %w", entry.Name(), err)
			}
		}
	}

	return nil
}

// parseGitHubURL parses a GitHub URL into its clone URL, branch, and subdirectory path.
// Supported formats:
//   - https://github.com/owner/repo/tree/branch/path/to/dir
//   - https://github.com/owner/repo
//
// Branch names containing slashes (e.g. feature/my-branch) are supported when
// encoded as %2F in the URL. The raw (escaped) path is used for splitting so
// the encoded branch segment is preserved, then unescaped for the return value.
func parseGitHubURL(rawURL string) (cloneURL, branch, subPath string, err error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid URL: %w", err)
	}

	if u.Host != "github.com" {
		return "", "", "", fmt.Errorf("unsupported host %q, only github.com is supported", u.Host)
	}

	// Use EscapedPath so that percent-encoded segments (e.g. %2F in branch
	// names) are not decoded before splitting on "/".
	rawPath := u.EscapedPath()

	// Path is like /owner/repo or /owner/repo/tree/branch/sub/path
	parts := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(parts) < 2 {
		return "", "", "", fmt.Errorf("invalid GitHub URL: expected at least owner/repo in path")
	}

	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")
	cloneURL = fmt.Sprintf("https://github.com/%s/%s.git", owner, repo)

	// If URL contains /tree/<branch>/..., extract branch and subpath.
	// The branch segment is unescaped so encoded slashes (%2F) become real
	// slashes in the returned branch name.
	if len(parts) >= 4 && parts[2] == "tree" {
		branch, _ = url.PathUnescape(parts[3])
		if len(parts) > 4 {
			raw := strings.Join(parts[4:], "/")
			subPath, _ = url.PathUnescape(raw)
		}
	}

	return cloneURL, branch, subPath, nil
}

// copyDir recursively copies a directory tree, skipping symlinks.
func copyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		// Skip symlinks to prevent traversal attacks
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}

		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}

// copyFile copies a single regular file. The caller must ensure src is not a symlink.
func copyFile(src, dst string) error {
	// Verify the source is a regular file via Lstat (does not follow symlinks)
	srcInfo, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if srcInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing to copy symlink: %s", src)
	}

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

	return os.Chmod(dst, srcInfo.Mode().Perm())
}
