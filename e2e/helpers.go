//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// registryURL is set during TestMain setup and used by all tests that need the registry.
var registryURL string

// arctlBinary returns the absolute path to the pre-built arctl binary.
// Checks ARCTL_BINARY env var first, then falls back to ../bin/arctl.
// The path is resolved to an absolute path because exec.Command resolves
// relative paths relative to cmd.Dir, not the process working directory.
func arctlBinary(t *testing.T) string {
	t.Helper()
	bin := os.Getenv("ARCTL_BINARY")
	if bin == "" {
		bin = filepath.Join("..", "bin", "arctl")
	}
	abs, err := filepath.Abs(bin)
	if err != nil {
		t.Fatalf("Failed to resolve absolute path for arctl binary %q: %v", bin, err)
	}
	return abs
}

// ArctlResult holds the output from running arctl.
type ArctlResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Err      error
}

// RunArctl executes arctl with the given args in the given working directory.
// It logs the full command for transparency.
func RunArctl(t *testing.T, workDir string, args ...string) ArctlResult {
	t.Helper()
	bin := arctlBinary(t)
	t.Logf("Running: %s %s (in %s)", bin, strings.Join(args, " "), workDir)

	cmd := exec.Command(bin, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.Env = os.Environ()

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	result := ArctlResult{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
		Err:      err,
	}

	t.Logf("Exit code: %d", result.ExitCode)
	if result.Stdout != "" {
		t.Logf("Stdout:\n%s", result.Stdout)
	}
	if result.Stderr != "" {
		t.Logf("Stderr:\n%s", result.Stderr)
	}

	return result
}

// RequireSuccess asserts the command succeeded (exit code 0).
func RequireSuccess(t *testing.T, result ArctlResult) {
	t.Helper()
	if result.ExitCode != 0 {
		t.Fatalf("Expected exit code 0 but got %d.\nStdout: %s\nStderr: %s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
}

// RequireFailure asserts the command failed (non-zero exit code).
func RequireFailure(t *testing.T, result ArctlResult) {
	t.Helper()
	if result.ExitCode == 0 {
		t.Fatalf("Expected non-zero exit code but got 0.\nStdout: %s\nStderr: %s",
			result.Stdout, result.Stderr)
	}
}

// RequireOutputContains asserts stdout or stderr contains the given substring.
func RequireOutputContains(t *testing.T, result ArctlResult, substr string) {
	t.Helper()
	combined := result.Stdout + result.Stderr
	if !strings.Contains(combined, substr) {
		t.Fatalf("Expected output to contain %q but got:\nStdout: %s\nStderr: %s",
			substr, result.Stdout, result.Stderr)
	}
}

// RequireEnv skips the test if the given environment variable is not set.
func RequireEnv(t *testing.T, envVar string) string {
	t.Helper()
	val := os.Getenv(envVar)
	if val == "" {
		t.Skipf("Skipping: requires %s environment variable", envVar)
	}
	return val
}

// RegistryURL returns the agentregistry URL set during TestMain setup.
func RegistryURL(t *testing.T) string {
	t.Helper()
	if registryURL == "" {
		t.Fatal("registryURL not set -- infrastructure setup may have failed")
	}
	return registryURL
}

// FileExists returns true if the file at path exists.
func FileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	return err == nil
}

// RequireFileExists asserts the file exists at the given path.
func RequireFileExists(t *testing.T, path string) {
	t.Helper()
	if !FileExists(t, path) {
		t.Fatalf("Expected file to exist: %s", path)
	}
}

// RequireDirExists asserts the directory exists at the given path.
func RequireDirExists(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Expected directory to exist: %s (error: %v)", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("Expected %s to be a directory but it is a file", path)
	}
}

// RequireFileContains asserts the file at path contains the given substring.
func RequireFileContains(t *testing.T, path, substr string) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read file %s: %v", path, err)
	}
	if !strings.Contains(string(content), substr) {
		t.Fatalf("Expected file %s to contain %q but content is:\n%s", path, substr, string(content))
	}
}

// WaitForHealth polls a URL until it returns HTTP 200 or the timeout expires.
func WaitForHealth(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 5 * time.Second}

	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				t.Logf("Health check passed: %s", url)
				return
			}
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("Health check timed out after %v: %s", timeout, url)
}

// ListServersURL returns the full URL for the list-servers endpoint.
func ListServersURL(regURL string) string {
	return regURL + "/servers"
}

// RegistryGet performs an HTTP GET against the given URL and returns the response.
// It fails the test on any transport error.
func RegistryGet(t *testing.T, url string) *http.Response {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to GET %s: %v", url, err)
	}
	return resp
}

// RemoveDeploymentsByServerName lists all deployments from the registry and
// removes any whose ServerName matches the given name. This ensures the
// deployment record is cleaned up from the database so that ReconcileAll
// in subsequent tests does not try to reconcile stale deployments.
func RemoveDeploymentsByServerName(t *testing.T, regURL, serverName string) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(regURL + "/deployments")
	if err != nil {
		t.Logf("Warning: failed to list deployments: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Logf("Warning: failed to read deployments response: %v", err)
		return
	}

	var result struct {
		Deployments []struct {
			ID         string `json:"id"`
			ServerName string `json:"serverName"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Logf("Warning: failed to parse deployments response: %v", err)
		return
	}

	for _, dep := range result.Deployments {
		if dep.ServerName != serverName {
			continue
		}
		req, err := http.NewRequest(http.MethodDelete, regURL+"/deployments/"+dep.ID, nil)
		if err != nil {
			t.Logf("Warning: failed to create delete request for deployment %s: %v", dep.ID, err)
			continue
		}
		delResp, err := client.Do(req)
		if err != nil {
			t.Logf("Warning: failed to delete deployment %s: %v", dep.ID, err)
			continue
		}
		delResp.Body.Close()
		t.Logf("Removed deployment record %s (server=%s)", dep.ID, serverName)
	}
}

// UniqueNameWithPrefix generates a unique name with the given prefix using a timestamp.
// The name uses hyphens as separators (suitable for MCP servers, Kubernetes resources, etc.).
func UniqueNameWithPrefix(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano()%100000)
}

// UniqueAgentName generates a unique agent name that satisfies the CLI validation:
// must start with a letter, contain only letters and digits, minimum 2 characters.
func UniqueAgentName(prefix string) string {
	return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano()%100000)
}
