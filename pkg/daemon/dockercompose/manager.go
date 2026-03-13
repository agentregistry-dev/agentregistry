package dockercompose

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	internaldaemon "github.com/agentregistry-dev/agentregistry/internal/daemon"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/daemon"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"gopkg.in/yaml.v3"
)

const (
	defaultWaitTimeout = 30 * time.Second
)

// Config holds docker-compose-specific configuration for the daemon manager.
type Config struct {
	ProjectName    string // docker compose project name
	ContainerName  string // container name to check for running state
	ComposeYAML    string // docker-compose.yml content
	DockerRegistry string // image registry
	Version        string // image version
}

// DefaultConfig returns the default docker compose configuration for AgentRegistry OSS.
func DefaultConfig() Config {
	return Config{
		ProjectName:    "agentregistry",
		ContainerName:  "agentregistry-server",
		ComposeYAML:    internaldaemon.DockerComposeYaml,
		DockerRegistry: version.DockerRegistry,
		Version:        version.Version,
	}
}

// Manager implements types.DaemonManager using docker compose.
type Manager struct {
	config Config
	health daemon.HealthChecker
}

var _ types.DaemonManager = (*Manager)(nil)

// NewManager creates a new docker compose daemon manager with the given config.
func NewManager(config Config) *Manager {
	return &Manager{
		config: config,
		health: daemon.HealthChecker{
			BaseURL: client.DefaultBaseURL,
		},
	}
}

func (m *Manager) IsRunning() bool {
	if m.health.IsResponding() {
		return true
	}
	return m.isContainerRunning()
}

func (m *Manager) Start() error {
	if !utils.IsDockerComposeAvailable() {
		fmt.Println("Docker compose is not available. Please install docker compose and try again.")
		fmt.Println("See https://docs.docker.com/compose/install/ for installation instructions.")
		fmt.Println("Agent Registry uses docker compose to start the server and the agent gateway.")
		return fmt.Errorf("docker compose is not available")
	}

	fmt.Printf("Starting %s daemon...\n", m.config.ProjectName)
	// Pipe the docker-compose.yml via stdin to docker compose
	cmd := exec.Command("docker", "compose", "-p", m.config.ProjectName, "-f", "-", "up", "-d", "--wait")
	cmd.Stdin = strings.NewReader(m.getComposeYAML())
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", m.config.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", m.config.DockerRegistry))
	if byt, err := cmd.CombinedOutput(); err != nil {
		fmt.Printf("failed to start docker compose: %v, output: %s", err, string(byt))
		return fmt.Errorf("failed to start docker compose: %w", err)
	}

	fmt.Printf("✓ %s daemon started successfully\n", m.config.ProjectName)

	if err := m.health.WaitForReady(defaultWaitTimeout); err != nil {
		return err
	}

	return nil
}

func (m *Manager) isContainerRunning() bool {
	cmd := exec.Command("docker", "compose", "-p", m.config.ProjectName, "-f", "-", "ps")
	cmd.Stdin = strings.NewReader(m.config.ComposeYAML)
	cmd.Env = append(os.Environ(), fmt.Sprintf("VERSION=%s", m.config.Version), fmt.Sprintf("DOCKER_REGISTRY=%s", m.config.DockerRegistry))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), m.config.ContainerName)
}

// getComposeYAML returns the docker-compose YAML, potentially modified for macOS with local clusters.
// On macOS, it patches the kubeconfig to use host.docker.internal instead of localhost
// and disables TLS verification since the cert won't be valid for host.docker.internal.
// Writes patched kubeconfig to a temp file, and updates the compose mount path accordingly.
// This does not modify the original kubeconfig file on host machine.
func (m *Manager) getComposeYAML() string {
	if runtime.GOOS != "darwin" {
		return m.config.ComposeYAML
	}

	// Read the original kubeconfig
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return m.config.ComposeYAML
	}

	kubeconfigPath := filepath.Join(homeDir, ".kube", "config")
	content, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		// No kubeconfig exists
		return m.config.ComposeYAML
	}

	// Skip patching if it is not using a local cluster
	if !strings.Contains(string(content), "localhost") && !strings.Contains(string(content), "127.0.0.1") {
		return m.config.ComposeYAML
	}

	// Parse kubeconfig as YAML to selectively patch only local clusters
	var kubeconfig map[string]any
	if err := yaml.Unmarshal(content, &kubeconfig); err != nil {
		return m.config.ComposeYAML
	}

	if clusters, ok := kubeconfig["clusters"].([]any); ok {
		for _, c := range clusters {
			cluster, ok := c.(map[string]any)
			if !ok {
				continue
			}
			clusterData, ok := cluster["cluster"].(map[string]any)
			if !ok {
				continue
			}
			server, _ := clusterData["server"].(string)
			if strings.Contains(server, "localhost") || strings.Contains(server, "127.0.0.1") {
				// Patch server URL
				server = strings.ReplaceAll(server, "localhost", "host.docker.internal")
				server = strings.ReplaceAll(server, "127.0.0.1", "host.docker.internal")
				clusterData["server"] = server
				// Disable TLS verification and remove CA data
				clusterData["insecure-skip-tls-verify"] = true
				delete(clusterData, "certificate-authority-data")
				delete(clusterData, "certificate-authority")
			}
		}
	}

	patchedBytes, err := yaml.Marshal(kubeconfig)
	if err != nil {
		return m.config.ComposeYAML
	}

	arctlDir := filepath.Join(homeDir, ".arctl")
	if err := os.MkdirAll(arctlDir, 0755); err != nil {
		return m.config.ComposeYAML
	}
	kubeconfigPatchedPath := filepath.Join(arctlDir, "kubeconfig")
	if err := os.WriteFile(kubeconfigPatchedPath, patchedBytes, 0600); err != nil {
		return m.config.ComposeYAML
	}

	return strings.ReplaceAll(m.config.ComposeYAML,
		"~/.kube/config:/root/.kube/config",
		kubeconfigPatchedPath+":/root/.kube/config")
}
