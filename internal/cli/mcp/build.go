package mcp

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/build"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/stoewer/go-strcase"

	"github.com/spf13/cobra"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build MCP server as a Docker image",
	Long: `Build an MCP server from the current project.
	
This command will detect the project type and build the appropriate
MCP server Docker image.`,
	RunE: runBuild,
	Example: `  arctl mcp build                              # Build Docker image from current directory
  arctl mcp build --project-dir ./my-project   # Build Docker image from specific directory`,
}

var (
	buildTag      string
	buildPush     bool
	buildDir      string
	buildPlatform string
)

func init() {
	McpCmd.AddCommand(buildCmd)

	buildCmd.Flags().StringVarP(&buildTag, "tag", "t", "", "Docker image tag (alias for --output)")
	buildCmd.Flags().BoolVar(&buildPush, "push", false, "Push Docker image to registry")
	buildCmd.Flags().StringVarP(&buildDir, "project-dir", "d", "", "Build directory (default: current directory)")
	buildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
}

func runBuild(_ *cobra.Command, _ []string) error {
	// Determine build directory
	buildDirectory := buildDir
	if buildDirectory == "" {
		var err error
		buildDirectory, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	imageName := buildTag
	if imageName == "" {
		// Load project manifest
		manifestManager := manifest.NewManager(buildDirectory)
		if !manifestManager.Exists() {
			return fmt.Errorf(
				"mcp.yaml not found in %s. Run 'arctl mcp init' first or specify a valid path with --project-dir",
				buildDirectory,
			)
		}

		projectManifest, err := manifestManager.Load()
		if err != nil {
			return fmt.Errorf("failed to load project manifest: %w", err)
		}

		version := projectManifest.Version
		if version == "" {
			version = "latest"
		}
		imageName = fmt.Sprintf("%s:%s", strcase.KebabCase(projectManifest.Name), version)
	}

	// Execute build
	builder := build.New()
	opts := build.Options{
		ProjectDir: buildDirectory,
		Tag:        imageName,
		Platform:   buildPlatform,
		Verbose:    verbose,
	}

	if err := builder.Build(opts); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	if buildPush {
		fmt.Printf("Pushing Docker image %s...\n", imageName)
		if err := runDocker("push", imageName); err != nil {
			return fmt.Errorf("docker push failed: %w", err)
		}
		fmt.Printf("✅ Docker image pushed successfully\n")
	}

	return nil
}

func runDocker(args ...string) error {
	if verbose {
		fmt.Printf("Running: docker %s\n", strings.Join(args, " "))
	}
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
