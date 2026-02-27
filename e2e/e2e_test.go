//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/daemon"
)

const (
	e2eClusterName = "arctl-e2e"
	e2eKubeContext = "kind-" + e2eClusterName
)

func TestMain(m *testing.M) {
	log.SetPrefix("[e2e] ")
	log.SetFlags(log.Ltime)

	// Verify prerequisites
	checkPrerequisites()

	// Find project root
	projectRoot := findProjectRoot()
	log.Printf("Project root: %s", projectRoot)

	var cleanup func()

	if os.Getenv("E2E_SKIP_SETUP") == "true" {
		log.Printf("E2E_SKIP_SETUP=true, skipping infrastructure setup")
		registryURL = os.Getenv("ARCTL_API_BASE_URL")
		if registryURL == "" {
			log.Fatal("ARCTL_API_BASE_URL must be set when E2E_SKIP_SETUP=true")
		}
	} else {
		cleanup = setupInfrastructure(projectRoot)
	}

	// Log configuration
	log.Printf("Configuration:")
	log.Printf("  ARCTL_API_BASE_URL: %s", registryURL)
	log.Printf("  GOOGLE_API_KEY:     %s", maskEnv("GOOGLE_API_KEY"))
	log.Printf("  Cluster:            %s (context: %s)", e2eClusterName, e2eKubeContext)

	// Run tests
	code := m.Run()

	// Teardown
	if cleanup != nil && os.Getenv("E2E_SKIP_TEARDOWN") != "true" {
		cleanup()
	} else if os.Getenv("E2E_SKIP_TEARDOWN") == "true" {
		log.Printf("E2E_SKIP_TEARDOWN=true, keeping cluster %s", e2eClusterName)
	}

	os.Exit(code)
}

// checkPrerequisites verifies required tools are available.
func checkPrerequisites() {
	// Verify arctl binary
	if _, err := os.Stat(resolveArctlBinaryPath()); err != nil {
		log.Fatalf("arctl binary not found at %s\nBuild it first with: make build-cli", resolveArctlBinaryPath())
	}

	for _, tool := range []string{"docker", "kubectl"} {
		if _, err := exec.LookPath(tool); err != nil {
			log.Fatalf("%s not found in PATH -- required for e2e tests", tool)
		}
	}
	// kind is managed via go tool directives in go.mod;
	// verify it resolves correctly.
	cmd := exec.Command("go", "tool", "kind", "version")
	if err := cmd.Run(); err != nil {
		log.Fatalf("go tool kind not available -- check tool directives in go.mod: %v", err)
	}
}

// resolveArctlBinaryPath returns the absolute path to the pre-built arctl binary.
func resolveArctlBinaryPath() string {
	bin := os.Getenv("ARCTL_BINARY")
	if bin == "" {
		bin = filepath.Join("..", "bin", "arctl")
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		log.Fatalf("Failed to resolve arctl binary path %q: %v", bin, err)
	}
	return abs
}

// findProjectRoot returns the absolute path to the project root.
func findProjectRoot() string {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		log.Fatalf("Failed to find project root via git: %v", err)
	}
	root := filepath.Clean(string(out[:len(out)-1])) // trim trailing newline
	return root
}

// setupInfrastructure creates a Kind cluster with kagent, builds the server and
// agent gateway Docker images, then starts the agentregistry daemon by running
// "arctl version" (which auto-starts docker compose containers). Returns a cleanup function.
func setupInfrastructure(projectRoot string) func() {
	log.Printf("Setting up e2e infrastructure...")

	// Step 1: Create Kind cluster (includes local registry + MetalLB)
	log.Printf("Step 1/5: Creating Kind cluster %q...", e2eClusterName)
	runMake(projectRoot, "create-kind-cluster",
		"KIND_CLUSTER_NAME="+e2eClusterName)

	// Switch context explicitly to ensure kubectl uses the right cluster
	runShell(projectRoot, "kubectl", "config", "use-context", e2eKubeContext)

	// Step 2: Install kagent (required for agent/mcp deploy --runtime kubernetes)
	log.Printf("Step 2/5: Installing kagent...")
	installKagent(projectRoot)

	// Step 3: Wait for kagent to be ready
	log.Printf("Step 3/5: Waiting for kagent to be ready...")
	waitForKagent(projectRoot)

	// Step 4: Build Docker images (server + agent gateway, both needed for local deploys)
	log.Printf("Step 4/5: Building Docker images...")
	ensureDotEnv(projectRoot)
	runMake(projectRoot, "docker")

	// Step 5: Start the daemon via "arctl version" and wait for health
	log.Printf("Step 5/5: Starting daemon via arctl version...")
	registryURL = "http://localhost:12121/v0"
	os.Setenv("ARCTL_API_BASE_URL", registryURL)

	bin := resolveArctlBinaryPath()
	cmd := exec.Command(bin, "version")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: arctl version exited with error (daemon may still be starting): %v", err)
	}

	waitForHealthStartup("http://localhost:12121", 90*time.Second)
	log.Printf("Infrastructure ready. Registry URL: %s", registryURL)

	return func() {
		log.Printf("Tearing down e2e infrastructure...")

		stopDaemon()

		log.Printf("Deleting Kind cluster %q...", e2eClusterName)
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "tool", "kind", "delete", "cluster", "--name", e2eClusterName)
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Printf("Warning: failed to delete Kind cluster: %v", err)
		}
		log.Printf("Teardown complete.")
	}
}

// ensureDotEnv creates a .env file from .env.example if one doesn't exist.
// The server Dockerfile copies .env into the image.
func ensureDotEnv(projectRoot string) {
	envFile := filepath.Join(projectRoot, ".env")
	if _, err := os.Stat(envFile); os.IsNotExist(err) {
		log.Printf("  Creating .env from .env.example...")
		src := filepath.Join(projectRoot, ".env.example")
		data, err := os.ReadFile(src)
		if err != nil {
			log.Fatalf("Failed to read .env.example: %v", err)
		}
		if err := os.WriteFile(envFile, data, 0644); err != nil {
			log.Fatalf("Failed to create .env: %v", err)
		}
	}
}

// stopDaemon tears down the agentregistry daemon containers started via docker compose.
func stopDaemon() {
	log.Printf("Stopping agentregistry daemon...")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "compose", "-p", "agentregistry", "-f", "-", "down", "--volumes", "--remove-orphans")
	cmd.Stdin = strings.NewReader(daemon.DockerComposeYaml)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: failed to stop daemon: %v", err)
	}
}

// runMake runs a make target in the project root directory.
// Additional key=value pairs are passed as make arguments (which become
// make variables and are also exported to sub-processes).
func runMake(projectRoot, target string, vars ...string) {
	args := []string{target}
	args = append(args, vars...)

	cmd := exec.Command("make", args...)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	log.Printf("Running: make %s %v", target, vars)
	if err := cmd.Run(); err != nil {
		log.Fatalf("make %s failed: %v", target, err)
	}
}

// runShell runs a command in the project root directory.
func runShell(projectRoot, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()

	if err := cmd.Run(); err != nil {
		log.Fatalf("%s %v failed: %v", name, args, err)
	}
}

// installKagent downloads and installs kagent on the Kind cluster.
func installKagent(projectRoot string) {
	// Download kagent CLI if not already available
	if _, err := exec.LookPath("kagent"); err != nil {
		log.Printf("  Downloading kagent CLI...")
		cmd := exec.Command("bash", "-c",
			"curl -sL https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | bash")
		cmd.Dir = projectRoot
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			log.Fatalf("Failed to download kagent: %v", err)
		}
	}

	// Set fake API keys (kagent/agents require them but we don't need real inference)
	for _, key := range []string{"OPENAI_API_KEY", "GOOGLE_API_KEY"} {
		if os.Getenv(key) == "" {
			os.Setenv(key, "fake-key-for-e2e-tests")
		}
	}

	// Install kagent on the cluster
	log.Printf("  Running kagent install...")
	cmd := exec.Command("kagent", "install", "--namespace", "kagent", "--profile", "minimal")
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		log.Fatalf("kagent install failed: %v", err)
	}
}

// waitForKagent waits for kagent deployments to be ready.
func waitForKagent(projectRoot string) {
	log.Printf("  Waiting for kagent controller...")
	cmd := exec.Command("kubectl", "wait", "--for=condition=available",
		"--timeout=300s",
		"deployment", "-l", "app.kubernetes.io/name=kagent",
		"--namespace", "kagent",
		"--context", e2eKubeContext)
	cmd.Dir = projectRoot
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Printf("Warning: kagent not fully ready: %v", err)
	}
}

// waitForHealthStartup polls a URL until it returns HTTP 200 or the timeout expires.
// Used during setup (no *testing.T available).
func waitForHealthStartup(url string, timeout time.Duration) {
	client := &http.Client{Timeout: 3 * time.Second}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				log.Printf("Health check passed: %s", url)
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	log.Fatalf("Health check timed out after %v: %s", timeout, url)
}

func maskEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		return "(not set)"
	}
	if len(val) <= 8 {
		return "****"
	}
	return val[:4] + "****"
}

// TestArctlVersion verifies the "arctl version" command succeeds and
// returns version information for both the CLI and the server.
func TestArctlVersion(t *testing.T) {
	tmpDir := t.TempDir()
	result := RunArctl(t, tmpDir, "version")
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "arctl version")
	RequireOutputContains(t, result, "Server version:")
}

// TestDaemonContainersRunning verifies that the agentregistry daemon
// containers (server + postgres) are running after setup.
func TestDaemonContainersRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, container := range []string{"agentregistry-server", "agent-registry-postgres"} {
		cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", container)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("Failed to inspect container %s: %v", container, err)
		}
		if got := strings.TrimSpace(string(out)); got != "true" {
			t.Fatalf("Expected container %s to be running, got state: %s", container, got)
		}
	}
}

// TestRegistryHealth verifies the registry health endpoint responds with 200.
func TestRegistryHealth(t *testing.T) {
	WaitForHealth(t, "http://localhost:12121", 30*time.Second)

	resp := RegistryGet(t, fmt.Sprintf("http://localhost:12121/v0/version"))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 from version endpoint, got %d", resp.StatusCode)
	}
}
