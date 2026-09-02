//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/wait"
)

// ClusterName, KubeContext, and RegistryNamespace identify the environment
// prepared by `make setup-kind-cluster`.
var (
	ClusterName       = getEnv("KIND_CLUSTER_NAME", "agentregistry")
	KubeContext       = "kind-" + ClusterName
	RegistryNamespace = getEnv("KIND_NAMESPACE", "agentregistry")
)

// StartRegistry verifies prerequisites, points ARCTL_BINARY at the pre-built
// CLI, and returns the registry URL plus a cleanup function. portEnv names an
// env var that pins the port-forward's local port; unset picks a free port.
func StartRegistry(portEnv string) (string, func()) {
	checkPrerequisites()
	os.Setenv("ARCTL_BINARY", resolveArctlBinaryPath())
	url, cleanup := resolveRegistryURL(portEnv)
	os.Setenv("ARCTL_API_BASE_URL", url)
	return url, cleanup
}

func repoRoot() string {
	_, f, _, ok := runtime.Caller(0)
	if !ok {
		panic("runtime.Caller failed")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(f)))
}

// resolveRegistryURL waits for the agentregistry LoadBalancer service to get an IP,
// then returns the registry URL and a cleanup function. On macOS, MetalLB IPs are
// not routable from the Docker Desktop host, so it falls back to a kubectl port-forward.
func resolveRegistryURL(portEnv string) (string, func()) {
	const timeout = 2 * time.Minute
	const pollInterval = 3 * time.Second
	const servicePort = 12121

	var ip string
	var lastErr error

	err := wait.PollUntilContextTimeout(context.Background(), pollInterval, timeout, true, func(ctx context.Context) (bool, error) {
		cmd := exec.CommandContext(ctx, "kubectl", "--context", KubeContext, "-n", RegistryNamespace,
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
		return startPortForward(portEnv, servicePort)
	}
	return fmt.Sprintf("http://%s:%d/v0", ip, servicePort), func() {}
}

// startPortForward tunnels the agentregistry service to localhost so that tests
// can reach it when the MetalLB IP is not routable from the host (macOS + Docker Desktop).
// Returns the localhost URL once the forward is ready, and a stop function.
func startPortForward(portEnv string, servicePort int) (string, func()) {
	localPort := 0
	if v := os.Getenv(portEnv); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			localPort = p
		}
	}
	// An ephemeral default lets several test packages forward concurrently.
	if localPort == 0 {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			log.Fatalf("Failed to reserve a local port: %v", err)
		}
		localPort = listener.Addr().(*net.TCPAddr).Port
		listener.Close()
	}
	const pollInterval = 500 * time.Millisecond
	const readyTimeout = 30 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "kubectl",
		"--context", KubeContext,
		"-n", RegistryNamespace,
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
	if _, err := exec.LookPath("kubectl"); err != nil {
		log.Fatalf("kubectl not found in PATH -- required for e2e tests")
	}
	toolsModfile := filepath.Join(repoRoot(), "tools", "go.mod")
	if out, err := exec.Command("go", "tool", "-modfile="+toolsModfile, "kind", "version").CombinedOutput(); err != nil {
		log.Fatalf("kind not available via tools/go.mod -- required for e2e tests: %v\n%s", err, out)
	}
}

// resolveArctlBinaryPath returns the absolute path to the pre-built arctl binary.
func resolveArctlBinaryPath() string {
	bin := os.Getenv("ARCTL_BINARY")
	if bin == "" {
		bin = filepath.Join(repoRoot(), "bin", "arctl")
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		log.Fatalf("Failed to resolve arctl binary path %q: %v", bin, err)
	}
	return abs
}
