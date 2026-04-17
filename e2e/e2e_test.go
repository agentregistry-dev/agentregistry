//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
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

	var cleanupFns []func()

	registryURL = os.Getenv("ARCTL_API_BASE_URL")
	if IsK8sBackend() {
		var cleanup func()
		registryURL, cleanup = resolveRegistryURL()
		cleanupFns = append(cleanupFns, cleanup)
	}
	if registryURL == "" {
		log.Fatal("ARCTL_API_BASE_URL not set — run tests via `make test-e2e-docker` or `make test-e2e-k8s`")
	}
	os.Setenv("ARCTL_API_BASE_URL", registryURL)

	log.Printf("Configuration:")
	log.Printf("  ARCTL_API_BASE_URL: %s", registryURL)
	log.Printf("  GOOGLE_API_KEY:     %s", maskEnv("GOOGLE_API_KEY"))
	if IsK8sBackend() {
		log.Printf("  Cluster:            %s (context: %s)", e2eClusterName, e2eKubeContext)
	}

	code := m.Run()
	for i := len(cleanupFns) - 1; i >= 0; i-- {
		cleanupFns[i]()
	}
	os.Exit(code)
}

// resolveRegistryURL waits for the agentregistry LoadBalancer service to get an IP,
// then returns the registry URL and a cleanup function. On macOS, MetalLB IPs are
// not routable from the Docker Desktop host, so it falls back to a kubectl port-forward.
func resolveRegistryURL() (string, func()) {
	const timeout = 2 * time.Minute
	const pollInterval = 3 * time.Second
	const servicePort = 12121

	var ip string
	var lastErr error

	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		cmd := exec.CommandContext(ctx, "kubectl", "--context", e2eKubeContext, "-n", "agentregistry",
			"get", "svc", "agentregistry", "-o", "jsonpath={.status.loadBalancer.ingress[0].ip}")
		out, pollErr := cmd.Output()
		if pollErr != nil {
			lastErr = pollErr
			return false, nil
		}
		ip = strings.TrimSpace(string(out))
		return ip != "", nil
	})

	if err != nil && lastErr != nil {
		log.Fatalf("Failed to discover registry LoadBalancer IP within %s: %v", timeout, lastErr)
	}
	if err != nil || ip == "" {
		log.Fatalf("LoadBalancer IP not assigned to agentregistry service within %s — is MetalLB running?", timeout)
	}

	if runtime.GOOS == "darwin" {
		log.Printf("macOS: LoadBalancer IP %s not routable from host — using port-forward", ip)
		return startPortForward(servicePort)
	}
	return fmt.Sprintf("http://%s:%d/v0", ip, servicePort), func() {}
}

// startPortForward tunnels the agentregistry service to localhost so that tests
// can reach it when the MetalLB IP is not routable from the host (macOS + Docker Desktop).
// Returns the localhost URL once the forward is ready, and a stop function.
func startPortForward(servicePort int) (string, func()) {
	localPort := 18121 // default; override with E2E_LOCAL_PORT env var
	if v := os.Getenv("E2E_LOCAL_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			localPort = p
		}
	}
	const pollInterval = 500 * time.Millisecond
	const readyTimeout = 30 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", e2eKubeContext,
		"-n", "agentregistry",
		"port-forward",
		"svc/agentregistry",
		fmt.Sprintf("%d:%d", localPort, servicePort),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		cancel()
		log.Fatalf("Failed to start kubectl port-forward: %v", err)
	}
	log.Printf("Port-forward started (pid %d): localhost:%d → agentregistry:%d", cmd.Process.Pid, localPort, servicePort)

	stop := func() {
		cancel()
		if cmd.Process != nil {
			cmd.Process.Kill() //nolint:errcheck
		}
		_ = cmd.Wait()
	}

	localURL := fmt.Sprintf("http://localhost:%d/v0", localPort)
	client := &http.Client{Timeout: 2 * time.Second}

	if err := wait.PollUntilContextTimeout(ctx, pollInterval, readyTimeout, true, func(_ context.Context) (bool, error) {
		resp, err := client.Get(localURL)
		if err != nil {
			return false, nil
		}
		resp.Body.Close()
		return true, nil
	}); err != nil {
		stop()
		log.Fatalf("Port-forward did not become ready within %v at %s", readyTimeout, localURL)
	}

	log.Printf("Port-forward ready: %s", localURL)
	return localURL, stop
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
// containers (server + postgres) are running. Only applicable to the
// docker backend where containers are managed by docker compose.
func TestDaemonContainersRunning(t *testing.T) {
	if IsK8sBackend() {
		t.Skip("Skipping: docker-compose containers are not used in k8s backend")
	}

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
	regURL := RegistryURL(t)
	WaitForHealth(t, regURL+"/ping", 30*time.Second)

	resp := RegistryGet(t, regURL+"/version")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200 from version endpoint, got %d", resp.StatusCode)
	}
}
