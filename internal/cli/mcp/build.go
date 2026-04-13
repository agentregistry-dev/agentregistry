package mcp

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/build"
	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/annotations"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"

	"github.com/spf13/cobra"
)

var BuildCmd = &cobra.Command{
	Use:         "build",
	Annotations: map[string]string{annotations.SkipDaemonAnnotation: "true"},
	Short:       "Build MCP server as a Docker image",
	Long: `Build an MCP server from the current project.
	
This command will detect the project type and build the appropriate
MCP server Docker image.`,
	Args:          cobra.ExactArgs(1),
	RunE:          runBuild,
	SilenceUsage:  true,  // Don't show usage on deployment errors
	SilenceErrors: false, // Still show error messages
	Example: `  arctl mcp build                              # Build Docker image from current directory
  arctl mcp build ./my-project   # Build Docker image from specific directory`,
}

var (
	buildDockerImageName string
	buildPush            bool
	buildPlatform        string
)

func init() {
	BuildCmd.Flags().StringVarP(&buildDockerImageName, "image", "n", "", "Full image specification (e.g., docker.io/myorg/my-mcp:v1.0.0)")
	BuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push the image to the container registry, specififed by --image")
	BuildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform (e.g., linux/amd64,linux/arm64)")

	BuildCmd.MarkFlagRequired("image")
}

func runBuild(cmd *cobra.Command, args []string) error {
	// Determine build directory
	buildDirectory := args[0]

	imageName := buildDockerImageName
	if imageName == "" {
		var err error
		loader := manifest.NewManager(buildDirectory)
		imageName, err = common.GetImageNameFromManifest(loader)
		if err != nil {
			return fmt.Errorf("failed to determine image name from manifest (%s): %w", buildDirectory, err)
		}
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
		printer.PrintInfo(fmt.Sprintf("Pushing Docker image %s...", imageName))
		executor := docker.NewExecutor(false, "")
		if err := executor.Push(imageName); err != nil {
			return err
		}
	}

	return nil
}
