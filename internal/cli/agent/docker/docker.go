package docker

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
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
		fmt.Printf("Running: docker %s\n", strings.Join(args, " "))
		if e.WorkDir != "" {
			fmt.Printf("Working directory: %s\n", e.WorkDir)
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
	fmt.Printf("✅ Successfully built Docker image: %s\n", imageName)
	return nil
}

// Push pushes the provided image to its registry.
func (e *Executor) Push(imageName string) error {
	if err := e.Run("push", imageName); err != nil {
		return fmt.Errorf("docker push failed: %w", err)
	}
	fmt.Printf("✅ Successfully pushed Docker image: %s\n", imageName)
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
