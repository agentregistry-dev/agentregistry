package daemon

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/daemon"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// DefaultConfig returns the default configuration for the daemon (AgentRegistry OSS daemon)
func DefaultConfig() types.DaemonConfig {
	return types.DaemonConfig{
		ProjectName:    "agentregistry",
		ContainerName:  "agentregistry-server",
		ComposeYAML:    daemon.DockerComposeYaml,
		DockerRegistry: version.DockerRegistry,
		Version:        version.Version,
	}
}

// DefaultDaemonManager implements types.DaemonManager with configurable options
type DefaultDaemonManager struct {
	config types.DaemonConfig
}

// Ensure DefaultDaemonManager implements types.DaemonManager
var _ types.DaemonManager = (*DefaultDaemonManager)(nil)

func NewDaemonManager(config *types.DaemonConfig) *DefaultDaemonManager {
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

// getComposeYAML returns the docker-compose YAML, potentially modified for macOS with local clusters.
// On macOS, it patches the kubeconfig to use host.docker.internal instead of localhost
// and disables TLS verification since the cert won't be valid for host.docker.internal.
// Writes patched kubeconfig to a temp file, and updates the compose mount path accordingly.
// This does not modify the original kubeconfig file on host machine.
func (d *DefaultDaemonManager) getComposeYAML() string {
	if runtime.GOOS != "darwin" {
		return d.config.ComposeYAML
	}

	// Read the original kubeconfig
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return d.config.ComposeYAML
	}

	kubeconfigPath := filepath.Join(homeDir, ".kube", "config")
	content, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		// No kubeconfig exists
		return d.config.ComposeYAML
	}

	// Skip patching if it is not using a local cluster
	if !strings.Contains(string(content), "localhost") && !strings.Contains(string(content), "127.0.0.1") {
		return d.config.ComposeYAML
	}

	// Patch localhost/127.0.0.1 to host.docker.internal for Docker Desktop on macOS
	patched := strings.ReplaceAll(string(content), "localhost", "host.docker.internal")
	patched = strings.ReplaceAll(patched, "127.0.0.1", "host.docker.internal")

	// Replace certificate-authority-data with insecure-skip-tls-verify
	caRegex := regexp.MustCompile(`(?m)^\s*certificate-authority-data:.*$`)
	patched = caRegex.ReplaceAllString(patched, "    insecure-skip-tls-verify: true")

	tmpPath := "/tmp/arctl-kubeconfig"
	if err := os.WriteFile(tmpPath, []byte(patched), 0600); err != nil {
		return d.config.ComposeYAML
	}

	return strings.ReplaceAll(d.config.ComposeYAML,
		"~/.kube/config:/root/.kube/config",
		tmpPath+":/root/.kube/config")
}

func (d *DefaultDaemonManager) Start() error {
	fmt.Printf("Starting %s daemon...\n", d.config.ProjectName)
	// Pipe the docker-compose.yml via stdin to docker compose
	cmd := exec.Command("docker", "compose", "-p", d.config.ProjectName, "-f", "-", "up", "-d", "--wait")
	cmd.Stdin = strings.NewReader(d.getComposeYAML())
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", d.config.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", d.config.DockerRegistry))
	if byt, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start docker compose: %v, output: %s", err, string(byt))
		return fmt.Errorf("failed to start docker compose: %w", err)
	}

	fmt.Printf("✓ %s daemon started successfully\n", d.config.ProjectName)

	return nil
}

func (d *DefaultDaemonManager) IsRunning() bool {
	// First check if a server is responding on the API port (local or Docker)
	if isServerResponding() {
		return true
	}

	cmd := exec.Command("docker", "compose", "-p", d.config.ProjectName, "-f", "-", "ps")
	cmd.Stdin = strings.NewReader(d.getComposeYAML())
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", d.config.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", d.config.DockerRegistry))
	output, err := cmd.CombinedOutput()
	if err != nil {
		fmt.Printf("failed to check if daemon is running: %v, output: %s", err, string(output))
		return false
	}
	return strings.Contains(string(output), d.config.ContainerName)
}

// isServerResponding checks if the server is responding on port 12121
func isServerResponding() bool {
	client := &http.Client{Timeout: 2 * time.Second}

	const maxRetries = 3
	for i := range maxRetries {
		resp, err := client.Get("http://localhost:12121/v0/version")
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
		}
		if i < maxRetries-1 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return false
}

