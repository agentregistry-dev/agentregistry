//go:build e2e

package e2e

import (
	"context"
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

const (
	localDeployComposeProject = "agentregistry_runtime"
	deployTargetLocal         = "local"
	deployTargetKubernetes    = "kubernetes"
)

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
		name: deployTargetLocal,
		verify: func(t *testing.T, agentName string) {
			waitForComposeService(t, agentName, 60*time.Second)
		},
		cleanup: func(t *testing.T, _ string) {
			removeLocalDeployment(t)
		},
	},
	{
		name:     deployTargetKubernetes,
		deplArgs: []string{"--provider-id", "kubernetes-default", "--namespace", "default"},
		verify: func(t *testing.T, agentName string) {
			verifyKubernetesAgentDeploymentHealthy(t, agentName)
		},
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
		name: deployTargetLocal,
		verify: func(t *testing.T, _ string) {
			waitForComposeService(t, "agent_gateway", 60*time.Second)
		},
		cleanup: func(t *testing.T, _ string) {
			removeLocalDeployment(t)
		},
	},
	{
		name:     deployTargetKubernetes,
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

// skipUnsupportedDeployTarget skips deploy targets that do not match the active
// E2E backend mode.
func skipUnsupportedDeployTarget(t *testing.T, targetName string) {
	t.Helper()
	if targetName == deployTargetLocal && IsK8sBackend() {
		t.Skip("skipping local deploy target: E2E_BACKEND=k8s")
	}
	if targetName == deployTargetKubernetes && !IsK8sBackend() {
		t.Skip("skipping kubernetes deploy target: E2E_BACKEND=docker")
	}
}

func TestAgentDeployCreate(t *testing.T) {
	for _, target := range agentDeployTargets {
		t.Run(target.name, func(t *testing.T) {
			skipUnsupportedDeployTarget(t, target.name)

			regURL := RegistryURL(t)
			tmpDir := t.TempDir()
			agentName := UniqueAgentName("e2edpl" + target.name[:3])
			agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

			t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })
			if target.cleanup != nil {
				t.Cleanup(func() { target.cleanup(t, agentName) })
			}

			t.Run("init_and_build", func(t *testing.T) {
				t.Log("Initializing agent scaffold...")
				result := RunArctl(t, tmpDir,
					"agent", "init", "adk", "python",
					"--model-name", "gemini-2.5-flash",
					"--image", agentImage,
					agentName,
				)
				RequireSuccess(t, result)

				t.Log("Building agent Docker image...")
				result = RunArctl(t, tmpDir, "agent", "build", agentName,
					"--image", agentImage)
				RequireSuccess(t, result)
				if target.name == deployTargetKubernetes {
					t.Log("Loading image into Kind cluster...")
					loadDockerImageToKind(t, agentImage)
				}
			})

			t.Run("publish", func(t *testing.T) {
				t.Log("Publishing agent to registry...")
				agentDir := filepath.Join(tmpDir, agentName)
				result := RunArctl(t, tmpDir,
					"agent", "publish", agentDir,
					"--registry-url", regURL,
				)
				RequireSuccess(t, result)
			})

			t.Run("deploy_create", func(t *testing.T) {
				t.Logf("Deploying agent %q (target: %s)...", agentName, target.name)
				args := []string{
					"deploy", "create", agentName,
					"--type", "agent",
					"--registry-url", regURL,
				}
				args = append(args, target.deplArgs...)
				result := RunArctl(t, tmpDir, args...)
				RequireSuccess(t, result)
			})

			t.Run("deploy_list", func(t *testing.T) {
				verifyDeploymentInList(t, tmpDir, regURL, agentName, "agent")
			})

			if target.verify != nil {
				t.Run("verify", func(t *testing.T) {
					t.Logf("Verifying deployment health (target: %s)...", target.name)
					target.verify(t, agentName)
				})
			}
		})
	}
}

func TestMCPDeployCreate(t *testing.T) {
	for _, target := range mcpDeployTargets {
		t.Run(target.name, func(t *testing.T) {
			skipUnsupportedDeployTarget(t, target.name)

			regURL := RegistryURL(t)
			tmpDir := t.TempDir()
			mcpName := UniqueNameWithPrefix("e2e-dpl-" + target.name[:3])
			serverName := "e2e-test/" + mcpName
			version := "0.0.1-e2e"
			defaultImage := mcpName + ":0.1.0"

			// Delete any stale server entry from a previous interrupted run.
			RunArctl(t, tmpDir, "mcp", "delete", serverName, "--version", version, "--registry-url", regURL)
			t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, serverName) })
			t.Cleanup(func() {
				RunArctl(t, tmpDir, "mcp", "delete", serverName, "--version", version, "--registry-url", regURL)
			})
			if target.cleanup != nil {
				t.Cleanup(func() { target.cleanup(t, mcpName) })
			}
			CleanupDockerImage(t, defaultImage)

			t.Run("init_and_build", func(t *testing.T) {
				t.Log("Initializing MCP server scaffold...")
				result := RunArctl(t, tmpDir,
					"mcp", "init", "python", mcpName,
					"--non-interactive",
					"--no-git",
				)
				RequireSuccess(t, result)

				t.Log("Building MCP Docker image...")
				mcpDir := filepath.Join(tmpDir, mcpName)
				result = RunArctl(t, tmpDir, "mcp", "build", mcpDir,
					"--image", defaultImage)
				RequireSuccess(t, result)
				if target.name == deployTargetKubernetes {
					t.Log("Loading image into Kind cluster...")
					loadDockerImageToKind(t, defaultImage)
				}
			})

			t.Run("publish", func(t *testing.T) {
				t.Log("Publishing MCP server to registry...")
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

			t.Run("deploy_create", func(t *testing.T) {
				t.Logf("Deploying MCP server %q (target: %s)...", serverName, target.name)
				args := []string{
					"deploy", "create", serverName,
					"--type", "mcp",
					"--version", version,
					"--registry-url", regURL,
				}
				args = append(args, target.deplArgs...)
				result := RunArctl(t, tmpDir, args...)
				RequireSuccess(t, result)
			})

			t.Run("deploy_list", func(t *testing.T) {
				verifyDeploymentInList(t, tmpDir, regURL, serverName, "mcp")
			})

			if target.verify != nil {
				t.Run("verify", func(t *testing.T) {
					t.Logf("Verifying deployment health (target: %s)...", target.name)
					target.verify(t, mcpName)
				})
			}
		})
	}
}

// TestDeployDeleteLifecycleLocal exercises the local-provider CLI lifecycle:
// create a deployment, extract its ID via "deploy list", delete it via
// "arctl deploy delete <prefix>" (testing ID-prefix resolution), and verify
// it no longer appears in "deploy list".
func TestDeployDeleteLifecycleLocal(t *testing.T) {
	if IsK8sBackend() {
		t.Skip("skipping local deploy lifecycle test: E2E_BACKEND=k8s")
	}

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	agentName := UniqueAgentName("e2edeldpl")
	agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

	t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })
	t.Cleanup(func() { removeLocalDeployment(t) })

	// 1. Init, build, and publish
	result := RunArctl(t, tmpDir,
		"agent", "init", "adk", "python",
		"--model-name", "gemini-2.5-flash",
		"--image", agentImage,
		agentName,
	)
	RequireSuccess(t, result)

	result = RunArctl(t, tmpDir, "agent", "build", agentName,
		"--image", agentImage)
	RequireSuccess(t, result)

	agentDir := filepath.Join(tmpDir, agentName)
	result = RunArctl(t, tmpDir,
		"agent", "publish", agentDir,
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	// 2. Deploy (local provider)
	result = RunArctl(t, tmpDir,
		"deploy", "create", agentName,
		"--type", "agent",
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	// 3. Extract the deployment ID from "deploy list -o json"
	deploymentID := extractDeploymentID(t, tmpDir, regURL, agentName, "agent")
	if deploymentID == "" {
		t.Fatal("Could not find deployment ID after deploy create")
	}
	t.Logf("Deployment ID: %s", deploymentID)

	// 4. Delete using truncated ID prefix (tests ID-prefix resolution)
	prefix := deploymentID[:8]
	result = RunArctl(t, tmpDir,
		"deploy", "delete", prefix,
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "deleted")

	// 5. Verify deployment is gone from "deploy list"
	result = RunArctl(t, tmpDir,
		"deploy", "list",
		"--type", "agent",
		"-o", "json",
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	if strings.TrimSpace(result.Stdout) != "" {
		var remaining []struct {
			ID         string `json:"id"`
			ServerName string `json:"serverName"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &remaining); err == nil {
			for _, d := range remaining {
				if d.ID == deploymentID {
					t.Fatalf("Deployment %s still present after delete", deploymentID)
				}
			}
		}
	}

	// 6. Verify deleting the same ID again fails
	result = RunArctl(t, tmpDir,
		"deploy", "delete", prefix,
		"--registry-url", regURL,
	)
	RequireFailure(t, result)
}

// extractDeploymentID runs "deploy list -o json" and returns the ID of the
// deployment matching the given resource name and type.
func extractDeploymentID(t *testing.T, workDir, regURL, resourceName, resourceType string) string {
	t.Helper()

	result := RunArctl(t, workDir,
		"deploy", "list",
		"--type", resourceType,
		"-o", "json",
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	var deployments []struct {
		ID           string `json:"id"`
		ServerName   string `json:"serverName"`
		ResourceType string `json:"resourceType"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &deployments); err != nil {
		t.Fatalf("Failed to parse deploy list JSON: %v\nOutput: %s", err, result.Stdout)
	}

	for _, d := range deployments {
		if d.ServerName == resourceName && d.ResourceType == resourceType {
			return d.ID
		}
	}
	return ""
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

		// Deployment-scoped local runtime names append the deployment ID,
		// e.g. "<service>-<deployment-id>". Accept those for e2e verification.
		ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
		cmd2 := exec.CommandContext(ctx2, "docker", "ps",
			"--filter", projectFilter,
			"--filter", "status=running",
			"--format", `{{.Label "com.docker.compose.service"}}	{{.Names}}`)
		out2, err2 := cmd2.Output()
		cancel2()

		if err2 == nil {
			lines := strings.Split(strings.TrimSpace(string(out2)), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.SplitN(line, "\t", 2)
				if len(parts) == 0 {
					continue
				}
				actualService := strings.TrimSpace(parts[0])
				if actualService == serviceName || strings.HasPrefix(actualService, serviceName+"-") {
					containerName := ""
					if len(parts) > 1 {
						containerName = strings.TrimSpace(parts[1])
					}
					t.Logf("Service %q is running in project %s via deployment-scoped service %q (%s)",
						serviceName, localDeployComposeProject, actualService, containerName)
					return
				}
			}
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

func TestDeleteDeploymentRemovesKubernetesResources(t *testing.T) {
	if !IsK8sBackend() {
		t.Skip("skipping kubernetes deletion test: E2E_BACKEND=docker")
	}
	kubeContext := kubeContextForE2E(t)
	if !kubeContextReachable(kubeContext) {
		t.Fatalf("kube context %q is not reachable", kubeContext)
	}

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	agentName := UniqueAgentName("e2ek8sdel")
	agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

	t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })

	t.Log("Initializing and building agent...")
	result := RunArctl(t, tmpDir,
		"agent", "init", "adk", "python",
		"--model-name", "gemini-2.5-flash",
		"--image", agentImage,
		agentName,
	)
	RequireSuccess(t, result)

	result = RunArctl(t, tmpDir, "agent", "build", agentName)
	RequireSuccess(t, result)
	t.Log("Loading image into Kind cluster...")
	loadDockerImageToKind(t, agentImage)

	t.Log("Publishing agent to registry...")
	agentDir := filepath.Join(tmpDir, agentName)
	result = RunArctl(t, tmpDir,
		"agent", "publish", agentDir,
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	t.Log("Deploying agent to Kubernetes...")
	result = RunArctl(t, tmpDir,
		"deploy", "create", agentName,
		"--type", "agent",
		"--registry-url", regURL,
		"--provider-id", "kubernetes-default",
		"--namespace", "default",
	)
	RequireSuccess(t, result)

	t.Log("Waiting for deployment record and k8s resource to appear...")
	deploymentID := waitForSingleAgentDeploymentID(t, regURL, agentName, "kubernetes-default", 30*time.Second)
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "agents.kagent.dev", deploymentID, 1, 45*time.Second)

	t.Log("Deleting deployment via API...")
	deleteDeploymentByIDAPI(t, regURL, deploymentID)
	waitForAgentDeploymentCount(t, regURL, agentName, "kubernetes-default", 0, 30*time.Second)

	t.Log("Verifying k8s resources are cleaned up...")
	// Regression assertion: deleting deployment from registry must remove k8s runtime resources.
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "agents.kagent.dev", deploymentID, 0, 45*time.Second)
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "configmaps", deploymentID, 0, 45*time.Second)
	t.Log("k8s resources cleaned up successfully")
}

func waitForAgentDeploymentCount(t *testing.T, regURL, agentName, providerID string, expectedCount int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastDeployments []string
	var lastErr error

	for time.Now().Before(deadline) {
		deployments, err := listManagedAgentDeployments(regURL, agentName, providerID)
		if err == nil && len(deployments) == expectedCount {
			return
		}
		lastDeployments = deployments
		lastErr = err
		time.Sleep(2 * time.Second)
	}

	if lastErr != nil {
		t.Fatalf("timed out waiting for %d deployment row(s) for agent=%q providerId=%q: %v",
			expectedCount, agentName, providerID, lastErr)
	}
	t.Fatalf("timed out waiting for %d deployment row(s) for agent=%q providerId=%q; got %d (%v)",
		expectedCount, agentName, providerID, len(lastDeployments), lastDeployments)
}

func waitForSingleAgentDeploymentID(t *testing.T, regURL, agentName, providerID string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		deployments, err := listManagedAgentDeployments(regURL, agentName, providerID)
		if err == nil && len(deployments) == 1 {
			return deployments[0]
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("timed out waiting for one deployment id for agent=%q providerId=%q", agentName, providerID)
	return ""
}

func listManagedAgentDeployments(regURL, agentName, providerID string) ([]string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	url := fmt.Sprintf("%s/deployments?resourceType=agent&providerId=%s&resourceName=%s", regURL, providerID, agentName)
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload struct {
		Deployments []struct {
			ID           string `json:"id"`
			ServerName   string `json:"serverName"`
			ProviderID   string `json:"providerId"`
			ResourceType string `json:"resourceType"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	var ids []string
	for _, dep := range payload.Deployments {
		if dep.ServerName == agentName && dep.ProviderID == providerID && dep.ResourceType == "agent" {
			ids = append(ids, dep.ID)
		}
	}
	return ids, nil
}

func deleteDeploymentByIDAPI(t *testing.T, regURL, deploymentID string) {
	t.Helper()
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodDelete, regURL+"/deployments/"+deploymentID, nil)
	if err != nil {
		t.Fatalf("failed creating delete request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed deleting deployment %s: %v", deploymentID, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("delete deployment %s failed with status %d: %s", deploymentID, resp.StatusCode, string(body))
	}
}

func deleteAgentDeploymentsDirectlyInDB(t *testing.T, agentName, providerID string) {
	t.Helper()
	sql := fmt.Sprintf(
		"DELETE FROM deployments WHERE server_name = '%s' AND resource_type = 'agent' AND provider_id = '%s';",
		escapeSQLLiteral(agentName),
		escapeSQLLiteral(providerID),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "agent-registry-postgres",
		"psql", "-U", "agentregistry", "-d", "agentregistry", "-v", "ON_ERROR_STOP=1", "-c", sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to delete deployments directly in db: %v\n%s", err, string(out))
	}
	t.Logf("Deleted agent deployment rows from DB for %q (providerId=%s)", agentName, providerID)
}

func escapeSQLLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// verifyDeploymentInList runs "arctl deploy list" with JSON output and asserts
// that a deployment with the given resource name and type appears in the results.
func verifyDeploymentInList(t *testing.T, workDir, regURL, resourceName, resourceType string) {
	t.Helper()

	result := RunArctl(t, workDir,
		"deploy", "list",
		"--type", resourceType,
		"-o", "json",
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	var deployments []struct {
		ServerName   string `json:"serverName"`
		ResourceType string `json:"resourceType"`
		Status       string `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &deployments); err != nil {
		t.Fatalf("Failed to parse deploy list JSON output: %v\nOutput: %s", err, result.Stdout)
	}

	for _, d := range deployments {
		if d.ServerName == resourceName && d.ResourceType == resourceType {
			t.Logf("Found deployment: name=%s type=%s status=%s", d.ServerName, d.ResourceType, d.Status)
			return
		}
	}

	t.Fatalf("Expected deployment name=%q type=%q not found in deploy list output (%d deployments)",
		resourceName, resourceType, len(deployments))
}

func dockerContainerRunning(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Running}}", name)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "true"
}

func kubeContextForE2E(t *testing.T) string {
	t.Helper()

	if ctx := strings.TrimSpace(os.Getenv("ARCTL_E2E_KUBE_CONTEXT")); ctx != "" {
		return ctx
	}

	if os.Getenv("E2E_SKIP_SETUP") != "true" {
		return e2eKubeContext
	}

	ctx, err := currentKubeContext()
	if err != nil || strings.TrimSpace(ctx) == "" {
		t.Skip("unable to determine active kube context in E2E_SKIP_SETUP mode")
	}
	return strings.TrimSpace(ctx)
}

func loadDockerImageToKind(t *testing.T, imageRef string) {
	t.Helper()

	kubeContext := kubeContextForE2E(t)
	clusterName, err := kindClusterNameFromContext(kubeContext)
	if err != nil {
		t.Fatalf("resolve kind cluster name from context %q: %v", kubeContext, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", "tool", "kind", "load", "docker-image", imageRef, "--name", clusterName)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("load image %q into kind cluster %q failed: %v\n%s", imageRef, clusterName, err, string(out))
	}
}

func kindClusterNameFromContext(kubeContext string) (string, error) {
	const prefix = "kind-"
	if !strings.HasPrefix(kubeContext, prefix) {
		return "", fmt.Errorf("expected kind context, got %q", kubeContext)
	}
	clusterName := strings.TrimSpace(strings.TrimPrefix(kubeContext, prefix))
	if clusterName == "" {
		return "", fmt.Errorf("empty kind cluster name in context %q", kubeContext)
	}
	return clusterName, nil
}

func currentKubeContext() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "config", "current-context")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func kubeContextReachable(kubeContext string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "get", "ns", "--context", kubeContext)
	return cmd.Run() == nil
}

func waitForK8sResourceCountByDeploymentID(t *testing.T, kubeContext, namespace, resource, deploymentID string, expectedCount int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastCount int
	var lastErr error

	for time.Now().Before(deadline) {
		count, err := k8sResourceCountByDeploymentID(kubeContext, namespace, resource, deploymentID)
		if err == nil && count == expectedCount {
			return
		}
		lastCount = count
		lastErr = err
		time.Sleep(2 * time.Second)
	}

	if lastErr != nil {
		t.Fatalf("timed out waiting for %d %s with deployment-id=%q (ns=%q context=%q): %v",
			expectedCount, resource, deploymentID, namespace, kubeContext, lastErr)
	}
	t.Fatalf("timed out waiting for %d %s with deployment-id=%q (ns=%q context=%q); got %d",
		expectedCount, resource, deploymentID, namespace, kubeContext, lastCount)
}

func k8sResourceCountByDeploymentID(kubeContext, namespace, resource, deploymentID string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "get", resource,
		"--namespace", namespace,
		"--context", kubeContext,
		"-l", "aregistry.ai/deployment-id="+deploymentID,
		"-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	var payload struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return 0, err
	}
	return len(payload.Items), nil
}

func verifyKubernetesAgentDeploymentHealthy(t *testing.T, agentName string) {
	t.Helper()

	kubeContext := kubeContextForE2E(t)
	if !kubeContextReachable(kubeContext) {
		t.Fatalf("kube context %q is not reachable", kubeContext)
	}

	regURL := RegistryURL(t)
	t.Log("Waiting for deployment record in registry...")
	deploymentID := waitForSingleAgentDeploymentID(t, regURL, agentName, "kubernetes-default", 45*time.Second)
	t.Logf("Deployment ID: %s", deploymentID)

	t.Log("Waiting for Agent CR to appear in Kubernetes...")
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "agents.kagent.dev", deploymentID, 1, 60*time.Second)
	t.Log("Waiting for Agent CR condition Accepted=True...")
	waitForKagentAgentConditionByDeploymentID(t, kubeContext, "default", deploymentID, "Accepted", "True", 90*time.Second)
	t.Log("Waiting for Agent CR condition Ready=True...")
	waitForKagentAgentConditionByDeploymentID(t, kubeContext, "default", deploymentID, "Ready", "True", 120*time.Second)
	t.Log("Agent deployment is healthy")
}

func waitForKagentAgentConditionByDeploymentID(t *testing.T, kubeContext, namespace, deploymentID, conditionType, expectedStatus string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	lastState := ""

	for time.Now().Before(deadline) {
		status, debugState, err := getKagentAgentConditionByDeploymentID(kubeContext, namespace, deploymentID, conditionType)
		if err == nil && status == expectedStatus {
			return
		}
		if err != nil {
			lastState = err.Error()
		} else {
			lastState = debugState
		}
		t.Logf("  waiting for condition %s=%s: %s", conditionType, expectedStatus, lastState)
		time.Sleep(3 * time.Second)
	}

	dumpKagentAgentDebug(t, kubeContext, namespace, deploymentID)
	t.Fatalf("timed out waiting for kagent agent condition %s=%s for deployment-id=%s (context=%s). Last observed: %s",
		conditionType, expectedStatus, deploymentID, kubeContext, lastState)
}

func getKagentAgentConditionByDeploymentID(kubeContext, namespace, deploymentID, conditionType string) (status string, debugState string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "get", "agents.kagent.dev",
		"--namespace", namespace,
		"--context", kubeContext,
		"-l", "aregistry.ai/deployment-id="+deploymentID,
		"-o", "json")
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}

	var payload struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type    string `json:"type"`
					Status  string `json:"status"`
					Reason  string `json:"reason"`
					Message string `json:"message"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return "", "", err
	}
	if len(payload.Items) == 0 {
		return "", "no Agent CR found for deployment label", nil
	}
	agent := payload.Items[0]
	debugState = "agent=" + agent.Metadata.Name + " conditions="
	if len(agent.Status.Conditions) == 0 {
		debugState += "<none>"
		return "", debugState, nil
	}
	var condParts []string
	for _, cond := range agent.Status.Conditions {
		condParts = append(condParts, fmt.Sprintf("%s=%s", cond.Type, cond.Status))
		if cond.Type == conditionType {
			return cond.Status, fmt.Sprintf("%s reason=%s message=%s", strings.Join(condParts, ","), cond.Reason, cond.Message), nil
		}
	}
	return "", strings.Join(condParts, ","), nil
}

func dumpKagentAgentDebug(t *testing.T, kubeContext, namespace, deploymentID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	getCmd := exec.CommandContext(ctx, "kubectl", "get", "agents.kagent.dev",
		"--namespace", namespace,
		"--context", kubeContext,
		"-l", "aregistry.ai/deployment-id="+deploymentID,
		"-o", "yaml")
	if out, err := getCmd.CombinedOutput(); err == nil {
		t.Logf("kagent agent YAML for deployment-id=%s:\n%s", deploymentID, string(out))
	} else {
		t.Logf("failed to dump kagent agent YAML for deployment-id=%s: %v\n%s", deploymentID, err, string(out))
	}
}

// TestAgentDeployWithPrompts tests that deploying an agent whose manifest
// references a registry prompt correctly resolves the prompt content and
// makes it available at the deployment target (ConfigMap for Kubernetes,
// prompts.json volume for local/Docker).
func TestAgentDeployWithPrompts(t *testing.T) {
	for _, target := range agentDeployTargets {
		t.Run(target.name, func(t *testing.T) {
			skipUnsupportedDeployTarget(t, target.name)

			regURL := RegistryURL(t)
			tmpDir := t.TempDir()
			agentName := UniqueAgentName("e2eprm" + target.name[:3])
			agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)
			promptName := UniqueNameWithPrefix("e2e-agent-prompt")
			promptVersion := "1.0.0"
			promptContent := "You are a helpful coding assistant for e2e tests."

			t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })
			if target.cleanup != nil {
				t.Cleanup(func() { target.cleanup(t, agentName) })
			}
			t.Cleanup(func() {
				RunArctl(t, tmpDir,
					"prompt", "delete", promptName,
					"--version", promptVersion,
					"--registry-url", regURL,
				)
			})

			t.Run("publish_prompt", func(t *testing.T) {
				promptFile := filepath.Join(tmpDir, "system-prompt.txt")
				if err := os.WriteFile(promptFile, []byte(promptContent), 0644); err != nil {
					t.Fatalf("failed to write prompt file: %v", err)
				}
				result := RunArctl(t, tmpDir,
					"prompt", "publish", promptFile,
					"--name", promptName,
					"--version", promptVersion,
					"--description", "E2E prompt for agent deploy test",
					"--registry-url", regURL,
				)
				RequireSuccess(t, result)
			})

			t.Run("init_and_add_prompt", func(t *testing.T) {
				result := RunArctl(t, tmpDir,
					"agent", "init", "adk", "python",
					"--model-name", "gemini-2.5-flash",
					"--image", agentImage,
					agentName,
				)
				RequireSuccess(t, result)

				result = RunArctl(t, tmpDir,
					"agent", "add-prompt", "system-prompt",
					"--registry-prompt-name", promptName,
					"--registry-prompt-version", promptVersion,
					"--registry-url", regURL,
					"--project-dir", filepath.Join(tmpDir, agentName),
				)
				RequireSuccess(t, result)

				agentYaml := filepath.Join(tmpDir, agentName, "agent.yaml")
				RequireFileContains(t, agentYaml, promptName)
			})

			t.Run("build", func(t *testing.T) {
				result := RunArctl(t, tmpDir, "agent", "build", agentName,
					"--image", agentImage)
				RequireSuccess(t, result)
				if target.name == deployTargetKubernetes {
					loadDockerImageToKind(t, agentImage)
				}
			})

			t.Run("publish", func(t *testing.T) {
				agentDir := filepath.Join(tmpDir, agentName)
				result := RunArctl(t, tmpDir,
					"agent", "publish", agentDir,
					"--registry-url", regURL,
				)
				RequireSuccess(t, result)
			})

			t.Run("deploy_create", func(t *testing.T) {
				args := []string{
					"deploy", "create", agentName,
					"--type", "agent",
					"--registry-url", regURL,
				}
				args = append(args, target.deplArgs...)
				result := RunArctl(t, tmpDir, args...)
				RequireSuccess(t, result)
			})

			if target.verify != nil {
				t.Run("verify_running", func(t *testing.T) {
					target.verify(t, agentName)
				})
			}

			t.Run("verify_prompts", func(t *testing.T) {
				switch target.name {
				case deployTargetKubernetes:
					verifyKubernetesPromptsConfig(t, agentName, promptContent)
				case deployTargetLocal:
					verifyLocalPromptsConfig(t, agentName, promptContent)
				default:
					t.Fatalf("unsupported deploy target: %s", target.name)
				}
			})
		})
	}
}

// verifyKubernetesPromptsConfig checks that the ConfigMap created for the
// agent deployment contains a prompts.json entry with the expected content.
func verifyKubernetesPromptsConfig(t *testing.T, agentName, expectedContent string) {
	t.Helper()
	kubeContext := kubeContextForE2E(t)
	regURL := RegistryURL(t)

	deploymentID := waitForSingleAgentDeploymentID(t, regURL, agentName, "kubernetes-default", 45*time.Second)
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "configmaps", deploymentID, 1, 60*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", "get", "configmaps",
		"--namespace", "default",
		"--context", kubeContext,
		"-l", "aregistry.ai/deployment-id="+deploymentID,
		"-o", "json")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("failed to get configmaps: %v", err)
	}

	var payload struct {
		Items []struct {
			Data map[string]string `json:"data"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		t.Fatalf("failed to parse configmap response: %v", err)
	}
	if len(payload.Items) == 0 {
		t.Fatal("no configmaps found for deployment")
	}

	promptsJSON, ok := payload.Items[0].Data["prompts.json"]
	if !ok {
		t.Fatalf("configmap does not contain prompts.json key; available keys: %v",
			configMapDataKeys(payload.Items[0].Data))
	}

	assertPromptsJSONContains(t, promptsJSON, expectedContent)
}

// verifyLocalPromptsConfig checks that the prompts.json file was written to
// the local runtime directory for the deployed agent.
// The runtime directory is under /tmp/arctl-runtime-* on the host, which is
// bind-mounted into both the agentregistry server and agent containers.
func verifyLocalPromptsConfig(t *testing.T, agentName, expectedContent string) {
	t.Helper()

	pattern := filepath.Join("/tmp", "arctl-runtime-*", agentName, "*", "prompts.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("failed to glob for prompts.json: %v", err)
	}
	if len(matches) == 0 {
		t.Fatalf("no prompts.json found matching pattern %s", pattern)
	}

	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("failed to read %s: %v", matches[0], err)
	}

	assertPromptsJSONContains(t, string(data), expectedContent)
}

// assertPromptsJSONContains parses a prompts.json string and asserts that at
// least one entry has the given content.
func assertPromptsJSONContains(t *testing.T, promptsJSON, expectedContent string) {
	t.Helper()

	var prompts []struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(promptsJSON), &prompts); err != nil {
		t.Fatalf("failed to parse prompts.json: %v\nraw: %s", err, promptsJSON)
	}

	for _, p := range prompts {
		if p.Content == expectedContent {
			return
		}
	}
	t.Fatalf("expected prompt content %q not found in prompts.json: %v", expectedContent, prompts)
}

// configMapDataKeys returns the keys of a ConfigMap's Data map for diagnostics.
func configMapDataKeys(data map[string]string) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}
