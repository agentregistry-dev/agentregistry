//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const localDeployComposeProject = "agentregistry_runtime"

// deployTarget describes a deployment provider used by the table-driven deploy tests.
type deployTarget struct {
	name     string   // subtest name (e.g. "local", "kubernetes")
	deplArgs []string // extra args appended to the deploy command
	// verify is called after deploy succeeds; use it to assert the
	// deployment is actually running (e.g. check Docker containers).
	// resourceName is the agent/MCP name being deployed.
	verify func(t *testing.T, resourceName string)
	// cleanup tears down whatever the deploy created.
	cleanup func(t *testing.T, resourceName string)
}

var agentDeployTargets = []deployTarget{
	{
		name: "local",
		verify: func(t *testing.T, agentName string) {
			waitForComposeService(t, agentName, 60*time.Second)
		},
		cleanup: func(t *testing.T, _ string) {
			removeLocalDeployment(t)
		},
	},
	{
		name:     "kubernetes",
		deplArgs: []string{"--provider-id", "kubernetes-default", "--namespace", "default"},
		cleanup: func(t *testing.T, agentName string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, "kubectl", "delete", "deployment", agentName,
				"--namespace", "default",
				"--context", e2eKubeContext,
				"--ignore-not-found=true")
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Logf("Warning: failed to delete deployment %s: %v\n%s", agentName, err, string(out))
			}
		},
	},
}

var mcpDeployTargets = []deployTarget{
	{
		name: "local",
		verify: func(t *testing.T, _ string) {
			waitForComposeService(t, "agent_gateway", 60*time.Second)
		},
		cleanup: func(t *testing.T, _ string) {
			removeLocalDeployment(t)
		},
	},
	{
		name:     "kubernetes",
		deplArgs: []string{"--provider-id", "kubernetes-default", "--namespace", "default"},
		cleanup: func(t *testing.T, mcpName string) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			for _, kind := range []string{"deployment", "service", "configmap"} {
				cmd := exec.CommandContext(ctx, "kubectl", "delete", kind,
					"-l", fmt.Sprintf("app.kubernetes.io/name=%s", mcpName),
					"--namespace", "default",
					"--context", e2eKubeContext,
					"--ignore-not-found=true")
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Logf("Warning: failed to delete %s for %s: %v\n%s", kind, mcpName, err, string(out))
				}
			}
		},
	},
}

func TestAgentDeploy(t *testing.T) {
	for _, target := range agentDeployTargets {
		t.Run(target.name, func(t *testing.T) {
			regURL := RegistryURL(t)
			tmpDir := t.TempDir()
			agentName := UniqueAgentName("e2edpl" + target.name[:3])

			// Register cleanup at the parent level so it runs after all
			// subtests (including verify) complete, not after deploy alone.
			// Remove deployment record first (LIFO) so ReconcileAll in
			// subsequent tests doesn't try to reconcile stale deployments.
			t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })
			if target.cleanup != nil {
				t.Cleanup(func() { target.cleanup(t, agentName) })
			}

			t.Run("init_and_build", func(t *testing.T) {
				result := RunArctl(t, tmpDir,
					"agent", "init", "adk", "python",
					"--model-name", "gemini-2.5-flash",
					agentName,
				)
				RequireSuccess(t, result)

				result = RunArctl(t, tmpDir, "agent", "build", agentName)
				RequireSuccess(t, result)
			})

			t.Run("publish", func(t *testing.T) {
				agentDir := filepath.Join(tmpDir, agentName)
				result := RunArctl(t, tmpDir,
					"agent", "publish", agentDir,
					"--registry-url", regURL,
				)
				RequireSuccess(t, result)
			})

			t.Run("deploy", func(t *testing.T) {
				args := []string{"agent", "deploy", agentName, "--registry-url", regURL}
				args = append(args, target.deplArgs...)
				result := RunArctl(t, tmpDir, args...)
				RequireSuccess(t, result)
			})

			if target.verify != nil {
				t.Run("verify", func(t *testing.T) {
					target.verify(t, agentName)
				})
			}
		})
	}
}

func TestMCPDeploy(t *testing.T) {
	for _, target := range mcpDeployTargets {
		t.Run(target.name, func(t *testing.T) {
			regURL := RegistryURL(t)
			tmpDir := t.TempDir()
			mcpName := UniqueNameWithPrefix("e2e-dpl-" + target.name[:3])
			serverName := "e2e-test/" + mcpName
			version := "0.0.1-e2e"
			defaultImage := mcpName + ":0.1.0"

			// Register cleanup at the parent level so it runs after all
			// subtests (including verify) complete, not after deploy alone.
			// Remove deployment record first (LIFO) so ReconcileAll in
			// subsequent tests doesn't try to reconcile stale deployments.
			t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, serverName) })
			if target.cleanup != nil {
				t.Cleanup(func() { target.cleanup(t, mcpName) })
			}
			CleanupDockerImage(t, defaultImage)

			t.Run("init_and_build", func(t *testing.T) {
				result := RunArctl(t, tmpDir,
					"mcp", "init", "python", mcpName,
					"--non-interactive",
					"--no-git",
				)
				RequireSuccess(t, result)

				mcpDir := filepath.Join(tmpDir, mcpName)
				result = RunArctl(t, tmpDir, "mcp", "build", mcpDir)
				RequireSuccess(t, result)
			})

			t.Run("publish", func(t *testing.T) {
				result := RunArctl(t, tmpDir,
					"mcp", "publish", serverName,
					"--type", "oci",
					"--package-id", defaultImage,
					"--version", version,
					"--description", "E2E test MCP server for deploy",
					"--registry-url", regURL,
				)
				RequireSuccess(t, result)
			})

			t.Run("deploy", func(t *testing.T) {
				args := []string{"mcp", "deploy", serverName, "--version", version, "--registry-url", regURL}
				args = append(args, target.deplArgs...)
				result := RunArctl(t, tmpDir, args...)
				RequireSuccess(t, result)
			})

			if target.verify != nil {
				t.Run("verify", func(t *testing.T) {
					target.verify(t, mcpName)
				})
			}
		})
	}
}

// waitForComposeService polls until a container with the given service name in
// the agentregistry_runtime compose project is running, or fails after timeout.
// Uses docker ps with label filters instead of docker compose ps, because the
// compose file lives inside the server container and is not on the host.
func waitForComposeService(t *testing.T, serviceName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	projectFilter := "label=com.docker.compose.project=" + localDeployComposeProject
	serviceFilter := "label=com.docker.compose.service=" + serviceName

	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, "docker", "ps",
			"--filter", projectFilter,
			"--filter", serviceFilter,
			"--filter", "status=running",
			"--format", "{{.Names}}")
		out, err := cmd.Output()
		cancel()

		if err == nil && strings.TrimSpace(string(out)) != "" {
			t.Logf("Service %q is running in project %s: %s",
				serviceName, localDeployComposeProject, strings.TrimSpace(string(out)))
			return
		}
		time.Sleep(3 * time.Second)
	}

	// Dump all containers in the project for debugging before failing
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", "ps", "-a",
		"--filter", projectFilter,
		"--format", "table {{.Names}}\t{{.Image}}\t{{.Status}}")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Logf("Containers in project %s:\n%s", localDeployComposeProject, string(out))
	}

	t.Fatalf("Timed out waiting for service %q to be running (project %s, timeout %v)",
		serviceName, localDeployComposeProject, timeout)
}

// removeLocalDeployment removes all containers belonging to the local compose
// deployment project. Uses docker rm directly since the compose file is not on the host.
func removeLocalDeployment(t *testing.T) {
	t.Helper()
	t.Logf("Cleaning up local deployment...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	projectFilter := "label=com.docker.compose.project=" + localDeployComposeProject

	// List all container IDs in the project
	listCmd := exec.CommandContext(ctx, "docker", "ps", "-a", "-q", "--filter", projectFilter)
	out, err := listCmd.Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return
	}

	ids := strings.Fields(strings.TrimSpace(string(out)))
	rmArgs := append([]string{"rm", "-f"}, ids...)
	rmCmd := exec.CommandContext(ctx, "docker", rmArgs...)
	if out, err := rmCmd.CombinedOutput(); err != nil {
		t.Logf("Warning: failed to remove local deployment containers: %v\n%s", err, string(out))
	}
}
