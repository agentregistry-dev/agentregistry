package mcp

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/build"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/stoewer/go-strcase"

	"github.com/spf13/cobra"
)

var BuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build MCP server as a Docker image",
	Long: `Build an MCP server from the current project.
	
This command will detect the project type and build the appropriate
MCP server Docker image.`,
	Args: cobra.ExactArgs(1),
	RunE: runBuild,
	Example: `  arctl mcp build                              # Build Docker image from current directory
  arctl mcp build ./my-project   # Build Docker image from specific directory`,
}

var (
	buildTag      string
	buildPush     bool
	buildPlatform string
)

func init() {
	BuildCmd.Flags().StringVarP(&buildTag, "tag", "t", "", "Docker image tag (alias for --output)")
	BuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push Docker image to registry")
	BuildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")
}

func runBuild(cmd *cobra.Command, args []string) error {
	// Determine build directory
	buildDirectory := args[0]

	imageName := buildTag
	if imageName == "" {
		// Load project manifest
		manifestManager := manifest.NewManager(buildDirectory)
		if !manifestManager.Exists() {
			return fmt.Errorf(
				"mcp.yaml not found in %s. Run 'arctl mcp init' first or specify a valid path as your first argument",
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
	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
