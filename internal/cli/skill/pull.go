package skill

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var PullCmd = &cobra.Command{
	Use:   "pull <skill-name> [output-directory]",
	Short: "Pull a skill from the registry and extract it locally",
	Long: `Pull a skill's Docker image from the registry and extract its contents to a local directory.
	
If output-directory is not specified, it will be extracted to ./skills/<skill-name>`,
	Args: cobra.RangeArgs(1, 2),
	RunE: runPull,
}

func runPull(cmd *cobra.Command, args []string) error {
	apiClient, err := utils.EnsureRegistryConnection()
	if err != nil {
		return err
	}

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
	skillResp, err := apiClient.GetSkillByName(skillName)
	if err != nil {
		return fmt.Errorf("failed to fetch skill from registry: %w", err)
	}

	if skillResp == nil {
		return fmt.Errorf("skill '%s' not found in registry", skillName)
	}

	printer.PrintSuccess(fmt.Sprintf("Found skill: %s (version %s)", skillResp.Skill.Name, skillResp.Skill.Version))

	// 2. Find Docker package in skill
	var dockerImage string
	for _, pkg := range skillResp.Skill.Packages {
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
