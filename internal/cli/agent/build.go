package agent

import (
	"fmt"
	"os"

	kagentcli "github.com/kagent-dev/kagent/go/cli/cli/agent"
	kagentconfig "github.com/kagent-dev/kagent/go/cli/config"
	"github.com/spf13/cobra"
)

var BuildCmd = &cobra.Command{
	Use:   "build [project-directory]",
	Short: "Build a Docker images for an agent project",
	Long: `Build Docker images for an agent project created with the init command.

This command will look for a agent.yaml file in the specified project directory and build Docker images using docker build. The images can optionally be pushed to a registry.

Image naming:
- If --image is provided, it will be used as the full image specification (e.g., ghcr.io/myorg/my-agent:v1.0.0)
- Otherwise, defaults to localhost:5001/{agentName}:latest where agentName is loaded from agent.yaml

Examples:
arctl agent build ./my-agent
arctl agent build ./my-agent --image ghcr.io/myorg/my-agent:v1.0.0
arctl agent build ./my-agent --image ghcr.io/myorg/my-agent:v1.0.0 --push`,
	Args:    cobra.ExactArgs(1),
	RunE:    runBuild,
	Example: `arctl agent build ./my-agent`,
}

var (
	buildImage    string
	buildPush     bool
	buildPlatform string
)

func init() {
	BuildCmd.Flags().StringVar(&buildImage, "image", "", "Full image specification (e.g., ghcr.io/myorg/my-agent:v1.0.0)")
	BuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push the image to the registry")
	BuildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Target platform for Docker build (e.g., linux/amd64, linux/arm64)")
}

func runBuild(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	cfg := &kagentconfig.Config{}
	buildCfg := &kagentcli.BuildCfg{
		Config: cfg,
	}
	buildCfg.ProjectDir = args[0]

	if err := kagentcli.BuildCmd(buildCfg); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	return nil
}
