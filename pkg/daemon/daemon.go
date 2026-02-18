package daemon

import (
	_ "embed"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/daemon"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"gopkg.in/yaml.v3"
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
	if config != nil { //nolint:nestif
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

	// Parse kubeconfig as YAML to selectively patch only local clusters
	var kubeconfig map[string]any
	if err := yaml.Unmarshal(content, &kubeconfig); err != nil {
		return d.config.ComposeYAML
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
		return d.config.ComposeYAML
	}

	arctlDir := filepath.Join(homeDir, ".arctl")
	if err := os.MkdirAll(arctlDir, 0755); err != nil {
		return d.config.ComposeYAML
	}
	kubeconfigPatchedPath := filepath.Join(arctlDir, "kubeconfig")
	if err := os.WriteFile(kubeconfigPatchedPath, patchedBytes, 0600); err != nil {
		return d.config.ComposeYAML
	}

	return strings.ReplaceAll(d.config.ComposeYAML,
		"~/.kube/config:/root/.kube/config",
		kubeconfigPatchedPath+":/root/.kube/config")
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
