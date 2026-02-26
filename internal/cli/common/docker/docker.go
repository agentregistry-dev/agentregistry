package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/printer"
)

// Executor wraps docker CLI operations with a working directory and verbosity.
type Executor struct {
	Verbose bool
	WorkDir string
}

// NewExecutor returns a configured docker executor.
func NewExecutor(verbose bool, workDir string) *Executor {
	return &Executor{
		Verbose: verbose,
		WorkDir: workDir,
	}
}

// CheckAvailability ensures docker CLI and daemon are reachable.
func (e *Executor) CheckAvailability() error {
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker command not found in PATH. Please install Docker")
	}

	cmd := exec.Command("docker", "version", "--format", "{{.Server.Version}}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker daemon is not running or not accessible. Please start Docker Desktop or the Docker daemon")
	}
	return nil
}

// Run executes docker with the provided arguments.
func (e *Executor) Run(args ...string) error {
	if e.Verbose {
		printer.PrintInfo(fmt.Sprintf("Running: docker %s", strings.Join(args, " ")))
		if e.WorkDir != "" {
			printer.PrintInfo(fmt.Sprintf("Working directory: %s", e.WorkDir))
		}
	}

	cmd := exec.Command("docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if e.WorkDir != "" {
		cmd.Dir = e.WorkDir
	}
	return cmd.Run()
}

// Build runs docker build with the supplied tag, context, and additional args.
func (e *Executor) Build(imageName, context string, extraArgs ...string) error {
	args := []string{"build", "-t", imageName}
	args = append(args, extraArgs...)
	args = append(args, context)
	if err := e.Run(args...); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("Successfully built Docker image: %s", imageName))
	return nil
}

// Push pushes the provided image to its registry.
func (e *Executor) Push(imageName string) error {
	if err := e.Run("push", imageName); err != nil {
		return fmt.Errorf("docker push failed: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("Successfully pushed Docker image: %s", imageName))
	return nil
}

// ComposeCommand returns the docker compose invocation (docker compose vs docker-compose).
func ComposeCommand() []string {
	if _, err := exec.LookPath("docker"); err == nil {
		cmd := exec.Command("docker", "compose", "version")
		if err := cmd.Run(); err == nil {
			return []string{"docker", "compose"}
		}
	}
	return []string{"docker-compose"}
}

// ImageExistsLocally checks if an image exists in the local Docker cache.
// TODO: Extend Run to support quiet mode so this can use e.Run.
func (e *Executor) ImageExistsLocally(imageRef string) bool {
	cmd := exec.Command("docker", "image", "inspect", imageRef)
	return cmd.Run() == nil
}

// Pull pulls a Docker image.
func (e *Executor) Pull(imageRef string) error {
	if err := e.Run("pull", imageRef); err != nil {
		return fmt.Errorf("docker pull failed: %w", err)
	}
	return nil
}

// CreateContainer creates a container from an image without starting it and
// returns the container ID. It tries with an entrypoint override first, then
// falls back for minimal images without a shell.
// TODO: Extend Run to support capturing output so this can use e.Run.
func (e *Executor) CreateContainer(imageRef string) (string, error) {
	cmd := exec.Command("docker", "create", "--entrypoint", "/bin/sh", imageRef, "-c", "echo")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fallback := exec.Command("docker", "create", imageRef)
		output, err = fallback.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("create container from image: %w: %s", err, strings.TrimSpace(string(output)))
		}
	}
	containerID := strings.TrimSpace(string(output))
	if containerID == "" {
		return "", fmt.Errorf("docker create returned empty container id")
	}
	return containerID, nil
}

// CopyFromContainer copies files from a container path to a local path.
// TODO: Extend Run to support quiet mode so this can use e.Run.
func (e *Executor) CopyFromContainer(containerID, containerPath, localPath string) error {
	cmd := exec.Command("docker", "cp", containerID+":"+containerPath, localPath)
	if e.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker cp: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// RemoveContainer removes a container by ID.
// TODO: Extend Run to support quiet mode so this can use e.Run.
func (e *Executor) RemoveContainer(containerID string) error {
	cmd := exec.Command("docker", "rm", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker rm: %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}
