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
			agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

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
					"--image", agentImage,
					agentName,
				)
				RequireSuccess(t, result)

				result = RunArctl(t, tmpDir, "agent", "build", agentName,
					"--image", agentImage)
				RequireSuccess(t, result)
				if target.name == "kubernetes" {
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
				result = RunArctl(t, tmpDir, "mcp", "build", mcpDir,
					"--image", defaultImage)
				RequireSuccess(t, result)
				if target.name == "kubernetes" {
					loadDockerImageToKind(t, defaultImage)
				}
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
	kubeContext := kubeContextForE2E(t)
	if !kubeContextReachable(kubeContext) {
		t.Skipf("kube context %q is not reachable", kubeContext)
	}

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	agentName := UniqueAgentName("e2ek8sdel")
	agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

	t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })

	result := RunArctl(t, tmpDir,
		"agent", "init", "adk", "python",
		"--model-name", "gemini-2.5-flash",
		"--image", agentImage,
		agentName,
	)
	RequireSuccess(t, result)

	result = RunArctl(t, tmpDir, "agent", "build", agentName)
	RequireSuccess(t, result)
	loadDockerImageToKind(t, agentImage)

	agentDir := filepath.Join(tmpDir, agentName)
	result = RunArctl(t, tmpDir,
		"agent", "publish", agentDir,
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)

	result = RunArctl(t, tmpDir,
		"agent", "deploy", agentName,
		"--registry-url", regURL,
		"--provider-id", "kubernetes-default",
		"--namespace", "default",
	)
	RequireSuccess(t, result)

	deploymentID := waitForSingleAgentDeploymentID(t, regURL, agentName, "kubernetes-default", 30*time.Second)
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "agents.kagent.dev", deploymentID, 1, 45*time.Second)

	deleteDeploymentByIDAPI(t, regURL, deploymentID)
	waitForAgentDeploymentCount(t, regURL, agentName, "kubernetes-default", 0, 30*time.Second)

	// Regression assertion: deleting deployment from registry must remove k8s runtime resources.
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "agents.kagent.dev", deploymentID, 0, 45*time.Second)
	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "configmaps", deploymentID, 0, 45*time.Second)
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
		"psql", "-U", "agentregistry", "-d", "agent-registry", "-v", "ON_ERROR_STOP=1", "-c", sql)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to delete deployments directly in db: %v\n%s", err, string(out))
	}
	t.Logf("Deleted agent deployment rows from DB for %q (providerId=%s)", agentName, providerID)
}

func escapeSQLLiteral(value string) string {
	return strings.ReplaceAll(value, "'", "''")
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
	deploymentID := waitForSingleAgentDeploymentID(t, regURL, agentName, "kubernetes-default", 45*time.Second)

	waitForK8sResourceCountByDeploymentID(t, kubeContext, "default", "agents.kagent.dev", deploymentID, 1, 60*time.Second)
	waitForKagentAgentConditionByDeploymentID(t, kubeContext, "default", deploymentID, "Accepted", "True", 90*time.Second)
	waitForKagentAgentConditionByDeploymentID(t, kubeContext, "default", deploymentID, "Ready", "True", 120*time.Second)
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
