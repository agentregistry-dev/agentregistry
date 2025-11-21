package agent

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/docker"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/spf13/cobra"
)

var BuildCmd = &cobra.Command{
	Use:   "build [project-directory]",
	Short: "Build Docker images for an agent project",
	Long: `Build Docker images for an agent project created with the init command.

This command looks for agent.yaml in the specified directory, regenerates template artifacts,
and invokes docker build (plus optional push) for both the agent and any command-type MCP servers.`,
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
	projectDir := args[0]
	if err := validateProjectDir(projectDir); err != nil {
		return err
	}

	dockerfilePath := filepath.Join(projectDir, "Dockerfile")
	if _, err := os.Stat(dockerfilePath); err != nil {
		return fmt.Errorf("dockerfile not found in project directory: %s", dockerfilePath)
	}

	manifest, err := project.LoadManifest(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load agent.yaml: %w", err)
	}

	if err := project.RegenerateMcpTools(projectDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to regenerate mcp_tools.py: %w", err)
	}

	if err := project.RegenerateDockerCompose(projectDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to regenerate docker-compose.yaml: %w", err)
	}

	mainDocker := docker.NewExecutor(verbose, projectDir)
	if err := mainDocker.CheckAvailability(); err != nil {
		return fmt.Errorf("docker check failed: %w", err)
	}

	imageName := project.ConstructImageName(buildImage, manifest.Name)
	extraArgs := []string{}
	if buildPlatform != "" {
		extraArgs = append(extraArgs, "--platform", buildPlatform)
	}

	if err := mainDocker.Build(imageName, ".", extraArgs...); err != nil {
		return err
	}

	if buildPush {
		if err := mainDocker.Push(imageName); err != nil {
			return err
		}
	}

	if err := buildMCPServers(projectDir, manifest, extraArgs); err != nil {
		return err
	}

	if buildPush {
		if err := pushMCPServers(manifest); err != nil {
			return err
		}
	}

	return nil
}

func validateProjectDir(projectDir string) error {
	info, err := os.Stat(projectDir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("project directory does not exist: %s", projectDir)
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("project directory is not a directory: %s", projectDir)
	}
	return nil
}

func buildMCPServers(projectDir string, manifest *common.AgentManifest, extraArgs []string) error {
	if manifest == nil {
		return nil
	}

	for _, srv := range manifest.McpServers {
		if srv.Type != "command" {
			continue
		}

		serverDir := filepath.Join(projectDir, srv.Name)
		if _, err := os.Stat(serverDir); err != nil {
			if verbose {
				fmt.Printf("Skipping MCP server %s: directory not found\n", srv.Name)
			}
			continue
		}

		dockerfilePath := filepath.Join(serverDir, "Dockerfile")
		if _, err := os.Stat(dockerfilePath); err != nil {
			return fmt.Errorf("dockerfile not found for MCP server %s (%s)", srv.Name, dockerfilePath)
		}

		imageName := project.ConstructMCPServerImageName(manifest.Name, srv.Name)
		exec := docker.NewExecutor(verbose, serverDir)
		if err := exec.Build(imageName, ".", extraArgs...); err != nil {
			return fmt.Errorf("docker build failed for MCP server %s: %w", srv.Name, err)
		}
	}
	return nil
}

func pushMCPServers(manifest *common.AgentManifest) error {
	if manifest == nil {
		return nil
	}

	exec := docker.NewExecutor(verbose, "")
	for _, srv := range manifest.McpServers {
		if srv.Type != "command" {
			continue
		}
		imageName := project.ConstructMCPServerImageName(manifest.Name, srv.Name)
		if err := exec.Push(imageName); err != nil {
			return fmt.Errorf("docker push failed for MCP server %s: %w", srv.Name, err)
		}
	}
	return nil
}
