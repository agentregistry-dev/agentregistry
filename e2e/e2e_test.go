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
)

// e2eClusterName and e2eKubeContext are derived from KIND_CLUSTER_NAME so that
// tests target whatever cluster was set up externally by `make setup-kind-cluster`.
var (
	e2eClusterName = getEnv("KIND_CLUSTER_NAME", "agentregistry")
	e2eKubeContext = "kind-" + e2eClusterName
)

func TestMain(m *testing.M) {
	log.SetPrefix("[e2e] ")
	log.SetFlags(log.Ltime)

	checkPrerequisites()

	registryURL = os.Getenv("ARCTL_API_BASE_URL")
	if registryURL == "" {
		log.Fatal("ARCTL_API_BASE_URL not set — run tests via `make test-e2e-docker` or `make test-e2e-k8s`")
	}

	log.Printf("Configuration:")
	log.Printf("  ARCTL_API_BASE_URL: %s", registryURL)
	log.Printf("  GOOGLE_API_KEY:     %s", maskEnv("GOOGLE_API_KEY"))
	if IsK8sBackend() {
		log.Printf("  Cluster:            %s (context: %s)", e2eClusterName, e2eKubeContext)
	}

	os.Exit(m.Run())
}

// checkPrerequisites verifies required tools are available.
func checkPrerequisites() {
	if _, err := os.Stat(resolveArctlBinaryPath()); err != nil {
		log.Fatalf("arctl binary not found at %s\nBuild it first with: make build-cli", resolveArctlBinaryPath())
	}
	if _, err := exec.LookPath("docker"); err != nil {
		log.Fatalf("docker not found in PATH -- required for e2e tests")
	}
	if IsK8sBackend() {
		if _, err := exec.LookPath("kubectl"); err != nil {
			log.Fatalf("kubectl not found in PATH -- required for k8s e2e tests")
		}
		if out, err := exec.Command("go", "tool", "kind", "version").CombinedOutput(); err != nil {
			log.Fatalf("go tool kind not available -- required for k8s e2e tests: %v\n%s", err, out)
		}
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
// containers (server + postgres) are running.
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
