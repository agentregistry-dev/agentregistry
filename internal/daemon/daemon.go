package daemon

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

//go:embed docker-compose.yml
var dockerComposeYaml string

// DaemonConfig allows customization of the default daemon manager
type DaemonConfig struct {
	ProjectName    string // docker compose project name (default: "agentregistry")
	ContainerName  string // container name to check for running state (default: "agentregistry-server")
	ComposeYAML    string // docker-compose.yml content (default: embedded)
	DockerRegistry string // image registry (default: version.DockerRegistry)
	Version        string // image version (default: version.Version)
}

// DefaultConfig returns the default configuration for the daemon
func DefaultConfig() DaemonConfig {
	return DaemonConfig{
		ProjectName:    "agentregistry",
		ContainerName:  "agentregistry-server",
		ComposeYAML:    dockerComposeYaml,
		DockerRegistry: version.DockerRegistry,
		Version:        version.Version,
	}
}

// DefaultDaemonManager implements DaemonManager with configurable options
type DefaultDaemonManager struct {
	config DaemonConfig
}

// Ensure DefaultDaemonManager implements types.DaemonManager
var _ types.DaemonManager = (*DefaultDaemonManager)(nil)

func NewDaemonManager(config *DaemonConfig) *DefaultDaemonManager {
	cfg := DefaultConfig()
	if config != nil {
		if config.ProjectName != "" {
			cfg.ProjectName = config.ProjectName
		}
		if config.ContainerName != "" {
			cfg.ContainerName = config.ContainerName
		}
		if config.ComposeYAML != "" {
			cfg.ComposeYAML = config.ComposeYAML
		}
		if config.DockerRegistry != "" {
			cfg.DockerRegistry = config.DockerRegistry
		}
		if config.Version != "" {
			cfg.Version = config.Version
		}
	}
	return &DefaultDaemonManager{config: cfg}
}

func (d *DefaultDaemonManager) Start() error {
	fmt.Printf("Starting %s daemon...\n", d.config.ProjectName)
	// Pipe the docker-compose.yml via stdin to docker compose
	cmd := exec.Command("docker", "compose", "-p", d.config.ProjectName, "-f", "-", "up", "-d", "--wait")
	cmd.Stdin = strings.NewReader(d.config.ComposeYAML)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", d.config.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", d.config.DockerRegistry))
	if byt, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start docker compose: %v, output: %s", err, string(byt))
		return fmt.Errorf("failed to start docker compose: %w", err)
	}

	fmt.Printf("✓ %s daemon started successfully\n", d.config.ProjectName)

	return nil
}

func (d *DefaultDaemonManager) IsRunning() bool {
	cmd := exec.Command("docker", "compose", "-p", d.config.ProjectName, "-f", "-", "ps")
	cmd.Stdin = strings.NewReader(d.config.ComposeYAML)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", d.config.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", d.config.DockerRegistry))
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("failed to check if daemon is running: %v, output: %s", err, string(output))
		return false
	}
	return strings.Contains(string(output), d.config.ContainerName)
}

func (d *DefaultDaemonManager) IsDockerComposeAvailable() bool {
	cmd := exec.Command("docker", "compose", "version")
	_, err := cmd.CombinedOutput()
	return err == nil
}
