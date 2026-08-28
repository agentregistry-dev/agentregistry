//go:build e2e

package e2e

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	registryv1alpha1 "github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const (
	kagentControllerURL = "http://kagent-controller.kagent:8083"
	kagentNamespace     = "kagent"
	kagentMCPPackage    = "@modelcontextprotocol/server-memory"
	kagentMCPVersion    = "2026.7.4"
	kagentMCPServerName = "io.github.modelcontextprotocol/server-memory"
	kagentMCPPort       = 3000
	kagentMCPPath       = "/mcp"
	kagentRemoteIDKey   = "runtimes.agentregistry.solo.io/kagent/remoteId"
)

func TestKagentAgent(t *testing.T) {
	env := newKagentTestEnvironment(t)
	id := UniqueNameWithPrefix("kagent-agent")
	secretName := "e2e-" + id + "-secret"
	runtimeName := "e2e-" + id + "-runtime"
	modelName := "e2e-" + id + "-model"
	agentName := "e2e-" + id + "-agent"
	deploymentName := "e2e-" + id + "-deployment"
	docs := newKagentScenarioDocs(
		t,
		"agent",
		"Kagent Agent E2E scenario",
		id,
	)
	registerKagentObjectCleanup(t, "agents.kagent.dev", agentName)
	registerKagentResourceCleanup(t, env, [][]string{
		{"delete", "deployment", deploymentName},
		{"delete", "agent", agentName},
		{"delete", "model", modelName, "--tag", "e2e"},
		{"delete", "-f", filepath.Join(env.workDir, "runtime.yaml")},
	})

	docs.Step("Configure the Kagent runtime", "Create a Secret and a Kagent Runtime that reads its bearer token from the Secret API.")
	env.Apply("runtime.yaml", kagentRuntimeManifest(secretName, runtimeName))
	docs.Apply(kagentRuntimeManifest(secretName, runtimeName))

	docs.Step("Deploy an Agent", "Create a Model and Agent, then deploy the Agent to Kagent using modelRef.")
	env.Apply("agent.yaml", kagentAgentManifest(modelName, agentName, ""))
	env.Apply("agent-deployment.yaml", kagentAgentDeploymentManifest(deploymentName, agentName, runtimeName, modelName))
	docs.Apply(kagentAgentManifest(modelName, agentName, ""))
	docs.Apply(kagentAgentDeploymentManifest(deploymentName, agentName, runtimeName, modelName))
	waitForKagentDeploymentReady(t, env.workDir, env.registryURL, deploymentName)
	waitForKagentResourceCreated(t, "agents.kagent.dev", agentName)
	workload := waitForKagentWorkloadCreated(t, agentName)
	assertContainerEnvironment(t, workload, "MODEL_PROVIDER", "bedrock")
	assertContainerEnvironment(t, workload, "MODEL_NAME", "anthropic.claude-3-5-sonnet-20241022-v2:0")
	assertKagentDeploymentRemoteID(t, env, deploymentName, agentName)

	docs.Step("Reapply the Deployment", "Reapply the unchanged Deployment and verify Kagent does not replace the Agent.")
	metadata := kagentResourceMetadata(t, "agents.kagent.dev", agentName)
	env.Apply("agent-deployment.yaml", kagentAgentDeploymentManifest(deploymentName, agentName, runtimeName, modelName))
	docs.Apply(kagentAgentDeploymentManifest(deploymentName, agentName, runtimeName, modelName))
	assertKagentResourceStable(t, "agents.kagent.dev", agentName, metadata, 10*time.Second)

	docs.Step("Remove the Agent Deployment", "Delete the AgentRegistry Deployment and verify Kagent removes the Agent workload.")
	docs.Command("arctl delete deployment " + deploymentName)
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n kagent wait --for=delete agent/" + agentName + " --timeout=2m")
	docs.Command("arctl delete agent " + agentName)
	docs.Command("arctl delete model " + modelName + " --tag e2e")
	docs.Command("arctl delete -f - <<EOF\n" + kagentRuntimeManifest(secretName, runtimeName) + "\nEOF")
	env.DeleteDeployment(deploymentName)
	waitForKagentResourceDeleted(t, "agents.kagent.dev", agentName)
	waitForKagentWorkloadDeleted(t, agentName)
}

func TestKagentMCP(t *testing.T) {
	env := newKagentTestEnvironment(t)
	id := UniqueNameWithPrefix("kagent-mcp")
	secretName := "e2e-" + id + "-secret"
	runtimeName := "e2e-" + id + "-runtime"
	remoteName := "e2e-" + id + "-remote"
	remoteDeploymentName := "e2e-" + id + "-remote-deployment"
	sourceName := "e2e-" + id + "-source"
	sourceDeploymentName := "e2e-" + id + "-source-deployment"
	docs := newKagentScenarioDocs(
		t,
		"mcp",
		"Kagent MCP E2E scenario",
		id,
	)
	registerKagentObjectCleanup(t, "remotemcpservers.kagent.dev", remoteName)
	registerKagentObjectCleanup(t, "mcpservers.kagent.dev", sourceName)
	registerKagentResourceCleanup(t, env, [][]string{
		{"delete", "deployment", remoteDeploymentName},
		{"delete", "deployment", sourceDeploymentName},
		{"delete", "mcpserver", remoteName},
		{"delete", "mcpserver", sourceName},
		{"delete", "-f", filepath.Join(env.workDir, "runtime.yaml")},
	})

	docs.Step("Configure the Kagent runtime", "Create the Secret and Runtime shared by the MCPServer deployments.")
	env.Apply("runtime.yaml", kagentRuntimeManifest(secretName, runtimeName))
	docs.Apply(kagentRuntimeManifest(secretName, runtimeName))

	docs.Step("Deploy a remote MCPServer", "Deploy a streamable HTTP MCPServer and verify Kagent creates a RemoteMCPServer.")
	env.Apply("remote-mcp.yaml", kagentRemoteMCPManifest(remoteName))
	env.Apply("remote-mcp-deployment.yaml", kagentMCPDeploymentManifest(remoteDeploymentName, remoteName, runtimeName))
	docs.Apply(kagentRemoteMCPManifest(remoteName))
	docs.Apply(kagentMCPDeploymentManifest(remoteDeploymentName, remoteName, runtimeName))
	waitForKagentDeploymentReady(t, env.workDir, env.registryURL, remoteDeploymentName)
	waitForKagentResourceCreated(t, "remotemcpservers.kagent.dev", remoteName)
	assertKagentDeploymentRemoteID(t, env, remoteDeploymentName, remoteName)

	docs.Step("Deploy a source-backed MCPServer", "Deploy a pinned npm stdio MCPServer and verify its live tools/list response.")
	env.Apply("source-mcp.yaml", kagentSourceMCPManifest(sourceName))
	env.Apply("source-mcp-deployment.yaml", kagentMCPDeploymentManifest(sourceDeploymentName, sourceName, runtimeName))
	docs.Apply(kagentSourceMCPManifest(sourceName))
	docs.Apply(kagentMCPDeploymentManifest(sourceDeploymentName, sourceName, runtimeName))
	waitForKagentDeploymentReady(t, env.workDir, env.registryURL, sourceDeploymentName)
	waitForKagentResourceCreated(t, "mcpservers.kagent.dev", sourceName)
	waitForKagentWorkloadAvailable(t, sourceName)
	assert.Contains(t, listKagentMCPTools(t, sourceName), "create_entities")
	assertKagentDeploymentRemoteID(t, env, sourceDeploymentName, sourceName)

	docs.Step("Remove the MCPServer Deployments", "Delete both Deployments and verify Kagent removes both MCPServer resources.")
	docs.Command("arctl delete deployment " + remoteDeploymentName)
	docs.Command("arctl delete deployment " + sourceDeploymentName)
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n kagent wait --for=delete remotemcpserver/" + remoteName + " --timeout=2m")
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n kagent wait --for=delete mcpserver/" + sourceName + " --timeout=2m")
	docs.Command("arctl delete mcpserver " + remoteName)
	docs.Command("arctl delete mcpserver " + sourceName)
	docs.Command("arctl delete -f - <<EOF\n" + kagentRuntimeManifest(secretName, runtimeName) + "\nEOF")
	env.DeleteDeployment(remoteDeploymentName)
	env.DeleteDeployment(sourceDeploymentName)
	waitForKagentResourceDeleted(t, "remotemcpservers.kagent.dev", remoteName)
	waitForKagentResourceDeleted(t, "mcpservers.kagent.dev", sourceName)
	waitForKagentWorkloadDeleted(t, sourceName)
}

func TestKagentDiscovery(t *testing.T) {
	env := newKagentTestEnvironment(t)
	id := UniqueNameWithPrefix("kagent-discovery")
	secretName := "e2e-" + id + "-secret"
	runtimeName := "e2e-" + id + "-runtime"
	agentName := "e2e-" + id + "-agent"
	docs := newKagentScenarioDocs(
		t,
		"discovery",
		"Kagent discovery E2E scenario",
		id,
	)
	cleanupRegistry := registerKagentResourceCleanup(t, env, [][]string{
		{"delete", "-f", filepath.Join(env.workDir, "runtime.yaml")},
	})
	registerKagentObjectCleanup(t, "agents.kagent.dev", agentName)

	docs.Step("Configure the Kagent runtime", "Create the Runtime through which AgentRegistry discovers out-of-band Kagent resources.")
	env.Apply("runtime.yaml", kagentRuntimeManifest(secretName, runtimeName))
	docs.Apply(kagentRuntimeManifest(secretName, runtimeName))

	oobManifest := kagentOutOfBandAgentManifest(agentName)
	docs.Step("Create an out-of-band Kagent Agent", "Apply an Agent directly to Kagent instead of creating an AgentRegistry Deployment.")
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} apply -f - <<EOF\n" + oobManifest + "\nEOF")
	applyKagentObject(t, "out-of-band-agent.yaml", oobManifest)
	discovered := waitForDiscoveredKagentDeployment(t, env.registryURL, runtimeName, agentName, true)
	require.Equal(t, registryv1alpha1.KindAgent, discovered.Spec.TargetRef.Kind)
	require.Equal(t, registryv1alpha1.DeploymentOriginDiscovered, discovered.Metadata.Annotations[registryv1alpha1.DeploymentOriginAnnotation])

	docs.Step("Remove the discovered Agent", "Delete the out-of-band Agent and its Runtime, then verify the discovered Deployment is removed.")
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n kagent delete agent " + agentName)
	docs.Command("arctl delete -f - <<EOF\n" + kagentRuntimeManifest(secretName, runtimeName) + "\nEOF")
	deleteKagentObject(t, "agents.kagent.dev", agentName)
	cleanupRegistry()
	waitForDiscoveredKagentDeployment(t, env.registryURL, runtimeName, agentName, false)
}

func TestKagentAgentMCP(t *testing.T) {
	registryURL := RegistryURL(t)
	workDir := t.TempDir()
	id := UniqueNameWithPrefix("kagent")
	runtimeName := "e2e-" + id + "-runtime"
	secretName := "e2e-" + id + "-secret"
	modelName := "e2e-" + id + "-model"
	agentName := "e2e-" + id + "-agent"
	agentDeploymentName := "e2e-" + id + "-agent-deployment"
	mcpName := "e2e-" + id + "-mcp"
	mcpDeploymentName := "e2e-" + id + "-mcp-deployment"

	names := kagentLifecycleNames{
		Secret:        secretName,
		Runtime:       runtimeName,
		Model:         modelName,
		Agent:         agentName,
		AgentDeploy:   agentDeploymentName,
		MCPServer:     mcpName,
		MCPDeployment: mcpDeploymentName,
	}
	manifest := kagentLifecycleManifest(names)
	docs := newKagentScenarioDocs(
		t,
		"agent-mcp",
		"Kagent Agent with MCP E2E scenario",
		id,
	)
	registerKagentObjectCleanup(t, "agents.kagent.dev", agentName)
	registerKagentObjectCleanup(t, "mcpservers.kagent.dev", mcpName)
	docs.Step(
		"Deploy an Agent with an MCPServer",
		"Create a secret-backed Kagent Runtime, deploy a source-backed MCPServer, and deploy an Agent that references it without deploymentRefs.",
	)
	docs.Apply(manifest)
	manifestPath := filepath.Join(workDir, "kagent-lifecycle.yaml")
	require.NoError(t, os.WriteFile(manifestPath, []byte(manifest), 0o600))
	require.NoError(
		t,
		os.WriteFile(
			filepath.Join(workDir, "runtime.yaml"),
			[]byte(kagentRuntimeManifest(secretName, runtimeName)),
			0o600,
		),
	)
	registerKagentRegistryCleanup(t, workDir, registryURL, names)

	RequireSuccess(t, RunArctl(t, workDir, "apply", "-f", manifestPath, "--registry-url", registryURL))
	waitForKagentDeploymentReady(t, workDir, registryURL, mcpDeploymentName)
	waitForKagentDeploymentReady(t, workDir, registryURL, agentDeploymentName)

	waitForKagentResourceCreated(t, "agents.kagent.dev", agentName)
	agentWorkload := waitForKagentWorkloadCreated(t, agentName)
	assertContainerEnvironment(t, agentWorkload, "HOST", "0.0.0.0")
	assertContainerEnvironment(t, agentWorkload, "KAGENT_NAMESPACE", kagentNamespace)
	assertContainerEnvironment(t, agentWorkload, "KAGENT_NAME", agentName)
	assertContainerEnvironment(t, agentWorkload, "KAGENT_URL", kagentControllerURL)
	assertContainerEnvironment(t, agentWorkload, "MODEL_PROVIDER", "bedrock")
	assertContainerEnvironment(t, agentWorkload, "MODEL_NAME", "anthropic.claude-3-5-sonnet-20241022-v2:0")

	waitForKagentResourceCreated(t, "mcpservers.kagent.dev", mcpName)
	waitForKagentWorkloadAvailable(t, mcpName)
	assert.Contains(t, listKagentMCPTools(t, mcpName), "create_entities")

	mcpConfig := containerEnvironment(t, agentWorkload, "MCP_SERVERS_CONFIG")
	require.NotEmpty(t, mcpConfig)
	assertKagentMCPConfig(t, mcpConfig, mcpDeploymentName, mcpName)

	docs.Step(
		"Remove the deployments",
		"Delete both AgentRegistry Deployments and verify that Kagent removes their resources.",
	)
	docs.Command("arctl delete deployment " + agentDeploymentName)
	docs.Command("arctl delete deployment " + mcpDeploymentName)
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n kagent wait --for=delete agent/" + agentName + " --timeout=2m")
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n kagent wait --for=delete mcpserver/" + mcpName + " --timeout=2m")
	docs.Command("arctl delete agent " + agentName)
	docs.Command("arctl delete mcpserver " + mcpName)
	docs.Command("arctl delete model " + modelName + " --tag e2e")
	docs.Command("arctl delete -f - <<EOF\n" + kagentRuntimeManifest(secretName, runtimeName) + "\nEOF")
	RequireSuccess(t, RunArctl(t, workDir, "delete", "deployment", agentDeploymentName, "--registry-url", registryURL))
	RequireSuccess(t, RunArctl(t, workDir, "delete", "deployment", mcpDeploymentName, "--registry-url", registryURL))
	waitForKagentResourceDeleted(t, "agents.kagent.dev", agentName)
	waitForKagentResourceDeleted(t, "mcpservers.kagent.dev", mcpName)
	waitForKagentWorkloadDeleted(t, agentName)
	waitForKagentWorkloadDeleted(t, mcpName)
}

func newKagentScenarioDocs(t *testing.T, name, title, id string) *e2eDocRecorder {
	t.Helper()
	return newE2EDocRecorder(
		t,
		filepath.Join(testDir(), "scenarios", "kagent", name+".md"),
		title,
		map[string]string{id: "${E2E_ID}"},
	)
}

type kagentTestEnvironment struct {
	t           *testing.T
	workDir     string
	registryURL string
}

func newKagentTestEnvironment(t *testing.T) kagentTestEnvironment {
	t.Helper()
	return kagentTestEnvironment{
		t:           t,
		workDir:     t.TempDir(),
		registryURL: RegistryURL(t),
	}
}

func (e kagentTestEnvironment) Apply(fileName, manifest string) {
	e.t.Helper()
	path := filepath.Join(e.workDir, fileName)
	require.NoError(e.t, os.WriteFile(path, []byte(manifest), 0o600))
	RequireSuccess(e.t, RunArctl(e.t, e.workDir, "apply", "-f", path, "--registry-url", e.registryURL))
}

func (e kagentTestEnvironment) DeleteDeployment(name string) {
	e.t.Helper()
	e.DeleteResource("deployment", name)
}

func (e kagentTestEnvironment) DeleteResource(kind, name string) {
	e.t.Helper()
	RequireSuccess(e.t, RunArctl(e.t, e.workDir, "delete", kind, name, "--registry-url", e.registryURL))
}

func registerKagentResourceCleanup(
	t *testing.T,
	env kagentTestEnvironment,
	resources [][]string,
) func() {
	t.Helper()
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			for _, arguments := range resources {
				arguments = append(arguments, "--registry-url", env.registryURL)
				checkKagentCleanupResult(t, RunArctl(t, env.workDir, arguments...))
			}
		})
	}
	t.Cleanup(cleanup)
	return cleanup
}

func kagentRuntimeManifest(secretName, runtimeName string) string {
	return fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Secret
metadata:
  name: %s
spec:
  type: Opaque
  stringData:
    token: e2e-token
---
apiVersion: ar.dev/v1alpha1
kind: Runtime
metadata:
  name: %s
spec:
  type: Kagent
  config:
    kagentUrl: %s
    namespace: %s
    auth:
      secretRef:
        name: %s
        key: token
`, secretName, runtimeName, kagentControllerURL, kagentNamespace, secretName)
}

func kagentAgentManifest(modelName, agentName, mcpName string) string {
	mcpServers := ""
	if mcpName != "" {
		mcpServers = fmt.Sprintf("  mcpServers:\n  - name: %s\n", mcpName)
	}
	return fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Model
metadata:
  name: %s
  tag: e2e
spec:
  title: Kagent E2E model
  provider: bedrock
  model: anthropic.claude-3-5-sonnet-20241022-v2:0
  auth:
    strategy: runtime
  endpoint:
    region: us-west-2
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
spec:
  source:
    image: registry.k8s.io/pause:3.10
    protocol: A2A
%s`, modelName, agentName, mcpServers)
}

func kagentAgentDeploymentManifest(deploymentName, agentName, runtimeName, modelName string) string {
	return fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: %s
spec:
  targetRef:
    kind: Agent
    name: %s
  runtimeRef:
    kind: Runtime
    name: %s
  modelRef:
    name: %s
    tag: e2e
`, deploymentName, agentName, runtimeName, modelName)
}

func kagentRemoteMCPManifest(name string) string {
	return fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
spec:
  description: Kagent remote MCPServer E2E fixture
  remote:
    type: streamable-http
    url: https://mcp.deepwiki.com/mcp
    headers:
    - name: X-E2E
      value: "true"
`, name)
}

func kagentSourceMCPManifest(name string) string {
	return fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
spec:
  title: Kagent E2E MCP server
  description: Kagent source-backed MCPServer E2E fixture
  source:
    package:
      origin:
        type: npm
        identifier: "%s"
        npm:
          version: %s
          serverName: %s
      transport:
        type: stdio
`, name, kagentMCPPackage, kagentMCPVersion, kagentMCPServerName)
}

func kagentMCPDeploymentManifest(deploymentName, mcpName, runtimeName string) string {
	return fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: %s
spec:
  targetRef:
    kind: MCPServer
    name: %s
  runtimeRef:
    kind: Runtime
    name: %s
`, deploymentName, mcpName, runtimeName)
}

func kagentOutOfBandAgentManifest(name string) string {
	return fmt.Sprintf(`apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: %s
  namespace: %s
spec:
  type: BYO
  description: Out-of-band AgentRegistry E2E fixture
  byo:
    deployment:
      image: registry.k8s.io/pause:3.10
`, name, kagentNamespace)
}

func kagentResourceMetadata(t *testing.T, resource, name string) metav1.PartialObjectMetadata {
	t.Helper()
	cmd := exec.Command(
		"kubectl",
		"--context", e2eKubeContext,
		"--namespace", kagentNamespace,
		"get", resource, name,
		"--output", "json",
	)
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, strings.TrimSpace(string(output)))
	var object metav1.PartialObjectMetadata
	require.NoError(t, json.Unmarshal(output, &object))
	return object
}

func assertKagentResourceStable(
	t *testing.T,
	resource string,
	name string,
	want metav1.PartialObjectMetadata,
	duration time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(duration)
	for {
		got := kagentResourceMetadata(t, resource, name)
		require.Equal(t, want.GetUID(), got.GetUID())
		require.Equal(t, want.GetGeneration(), got.GetGeneration())
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(time.Second)
	}
}

func assertKagentDeploymentRemoteID(
	t *testing.T,
	env kagentTestEnvironment,
	deploymentName string,
	want string,
) {
	t.Helper()
	result := RunArctl(
		t,
		env.workDir,
		"get", "deployment", deploymentName,
		"--output", "json",
		"--registry-url", env.registryURL,
	)
	RequireSuccess(t, result)
	var deployment registryv1alpha1.Deployment
	require.NoError(t, json.Unmarshal([]byte(result.Stdout), &deployment))
	require.Equal(t, want, deployment.Metadata.Annotations[kagentRemoteIDKey])
}

func applyKagentObject(t *testing.T, fileName, manifest string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), fileName)
	require.NoError(t, os.WriteFile(path, []byte(manifest), 0o600))
	output, err := exec.Command("kubectl", "--context", e2eKubeContext, "apply", "-f", path).CombinedOutput()
	require.NoError(t, err, strings.TrimSpace(string(output)))
}

func deleteKagentObject(t *testing.T, resource, name string) {
	t.Helper()
	output, err := exec.Command(
		"kubectl",
		"--context", e2eKubeContext,
		"--namespace", kagentNamespace,
		"delete", resource, name,
		"--ignore-not-found",
	).CombinedOutput()
	require.NoError(t, err, strings.TrimSpace(string(output)))
}

func registerKagentObjectCleanup(t *testing.T, resource, name string) {
	t.Helper()
	t.Cleanup(func() {
		deleteKagentObject(t, resource, name)
	})
}

func waitForDiscoveredKagentDeployment(
	t *testing.T,
	registryURL string,
	runtimeName string,
	targetName string,
	present bool,
) *registryv1alpha1.Deployment {
	t.Helper()
	var matched *registryv1alpha1.Deployment
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		deployments, err := listDiscoveredDeployments(registryURL)
		if !assert.NoError(c, err) {
			return
		}
		matches := []registryv1alpha1.Deployment{}
		for _, deployment := range deployments {
			if deployment.Spec.RuntimeRef.Name == runtimeName && deployment.Spec.TargetRef.Name == targetName {
				matches = append(matches, deployment)
			}
		}
		if !assert.Len(c, matches, boolToCount(present)) || len(matches) == 0 {
			return
		}
		matched = &matches[0]
	}, 2*time.Minute, 2*time.Second)
	return matched
}

func boolToCount(value bool) int {
	if value {
		return 1
	}
	return 0
}

func listDiscoveredDeployments(registryURL string) ([]registryv1alpha1.Deployment, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(registryURL + "/deployments?origin=discovered&limit=100")
	if err != nil {
		return nil, fmt.Errorf("list discovered Deployments: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list discovered Deployments: HTTP %s", response.Status)
	}
	var body struct {
		Items []registryv1alpha1.Deployment `json:"items"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode discovered Deployments: %w", err)
	}
	return body.Items, nil
}

type kagentLifecycleNames struct {
	Secret        string
	Runtime       string
	Model         string
	Agent         string
	AgentDeploy   string
	MCPServer     string
	MCPDeployment string
}

func kagentLifecycleManifest(names kagentLifecycleNames) string {
	return fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Secret
metadata:
  name: %s
spec:
  type: Opaque
  stringData:
    token: e2e-token
---
apiVersion: ar.dev/v1alpha1
kind: Runtime
metadata:
  name: %s
spec:
  type: Kagent
  config:
    kagentUrl: %s
    namespace: %s
    auth:
      secretRef:
        name: %s
        key: token
---
apiVersion: ar.dev/v1alpha1
kind: Model
metadata:
  name: %s
  tag: e2e
spec:
  title: Kagent E2E model
  provider: bedrock
  model: anthropic.claude-3-5-sonnet-20241022-v2:0
  auth:
    strategy: runtime
  endpoint:
    region: us-west-2
---
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
spec:
  title: Kagent E2E MCP server
  description: Kagent source-backed MCP lifecycle fixture
  source:
    package:
      origin:
        type: npm
        identifier: "%s"
        npm:
          version: %s
          serverName: %s
      transport:
        type: stdio
---
apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: %s
spec:
  targetRef:
    kind: MCPServer
    name: %s
  runtimeRef:
    kind: Runtime
    name: %s
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
spec:
  source:
    image: registry.k8s.io/pause:3.10
    protocol: A2A
  mcpServers:
  - name: %s
---
apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: %s
spec:
  targetRef:
    kind: Agent
    name: %s
  runtimeRef:
    kind: Runtime
    name: %s
  modelRef:
    name: %s
    tag: e2e
`,
		names.Secret,
		names.Runtime,
		kagentControllerURL,
		kagentNamespace,
		names.Secret,
		names.Model,
		names.MCPServer,
		kagentMCPPackage,
		kagentMCPVersion,
		kagentMCPServerName,
		names.MCPDeployment,
		names.MCPServer,
		names.Runtime,
		names.Agent,
		names.MCPServer,
		names.AgentDeploy,
		names.Agent,
		names.Runtime,
		names.Model,
	)
}

func waitForKagentResourceCreated(t *testing.T, resource, name string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		cmd := exec.Command(
			"kubectl",
			"--context", e2eKubeContext,
			"--namespace", kagentNamespace,
			"get", resource, name,
			"--ignore-not-found",
			"--output", "name",
		)
		output, err := cmd.CombinedOutput()
		if !assert.NoError(c, err, strings.TrimSpace(string(output))) {
			return
		}
		assert.NotEmpty(c, strings.TrimSpace(string(output)))
	}, 2*time.Minute, 2*time.Second)
}

func waitForKagentResourceDeleted(t *testing.T, resource, name string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		cmd := exec.Command(
			"kubectl",
			"--context", e2eKubeContext,
			"--namespace", kagentNamespace,
			"get", resource, name,
			"--ignore-not-found",
			"--output", "json",
		)
		output, err := cmd.CombinedOutput()
		if !assert.NoError(c, err, strings.TrimSpace(string(output))) {
			return
		}
		if len(strings.TrimSpace(string(output))) == 0 {
			return
		}
		assert.Empty(c, strings.TrimSpace(string(output)))
	}, 2*time.Minute, 2*time.Second)
}

func waitForKagentDeploymentReady(t *testing.T, workDir, registryURL, name string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		result := RunArctl(
			t,
			workDir,
			"get", "deployment", name,
			"--output", "json",
			"--registry-url", registryURL,
		)
		if !assert.Zero(c, result.ExitCode, result.Stderr) {
			return
		}
		var deployment registryv1alpha1.Deployment
		if !assert.NoError(c, json.Unmarshal([]byte(result.Stdout), &deployment)) {
			return
		}
		ready := deployment.Status.GetCondition("Ready")
		if !assert.NotNil(c, ready) {
			return
		}
		assert.Equal(c, registryv1alpha1.ConditionTrue, ready.Status, ready.Message)
	}, 2*time.Minute, 2*time.Second, "AgentRegistry Deployment %s did not become Ready", name)
}

func waitForKagentWorkloadAvailable(t *testing.T, name string) *appsv1.Deployment {
	t.Helper()
	var deployment appsv1.Deployment
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		current, err := getKagentWorkload(name)
		if !assert.NoError(c, err) {
			return
		}
		deployment = current
		assert.Positive(c, deployment.Status.AvailableReplicas)
	}, 3*time.Minute, 2*time.Second)
	return &deployment
}

func waitForKagentWorkloadCreated(t *testing.T, name string) *appsv1.Deployment {
	t.Helper()
	var deployment appsv1.Deployment
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		current, err := getKagentWorkload(name)
		if !assert.NoError(c, err) {
			return
		}
		deployment = current
	}, 2*time.Minute, 2*time.Second)
	return &deployment
}

func waitForKagentWorkloadDeleted(t *testing.T, name string) {
	t.Helper()
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err := getKagentWorkload(name)
		assert.True(c, apierrors.IsNotFound(err), "get Deployment %s/%s: %v", kagentNamespace, name, err)
	}, 2*time.Minute, 2*time.Second)
}

func getKagentWorkload(name string) (appsv1.Deployment, error) {
	cmd := exec.Command(
		"kubectl",
		"--context", e2eKubeContext,
		"--namespace", kagentNamespace,
		"get", "deployment", name,
		"--output", "json",
	)
	output, err := cmd.Output()
	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok && strings.Contains(string(exitError.Stderr), "NotFound") {
			return appsv1.Deployment{}, apierrors.NewNotFound(
				schema.GroupResource{Group: "apps", Resource: "deployments"},
				name,
			)
		}
		return appsv1.Deployment{}, err
	}
	var deployment appsv1.Deployment
	if err := json.Unmarshal(output, &deployment); err != nil {
		return appsv1.Deployment{}, fmt.Errorf("decode Deployment %s/%s: %w", kagentNamespace, name, err)
	}
	return deployment, nil
}

func assertContainerEnvironment(t *testing.T, deployment *appsv1.Deployment, name, want string) {
	t.Helper()
	assert.Equal(t, want, containerEnvironment(t, deployment, name))
}

func containerEnvironment(t *testing.T, deployment *appsv1.Deployment, name string) string {
	t.Helper()
	require.NotEmpty(t, deployment.Spec.Template.Spec.Containers)
	for _, variable := range deployment.Spec.Template.Spec.Containers[0].Env {
		if variable.Name == name {
			return variable.Value
		}
	}
	t.Errorf("Kagent workload environment does not contain %s", name)
	return ""
}

func assertKagentMCPConfig(t *testing.T, raw, deploymentName, serverName string) {
	t.Helper()
	var servers []struct {
		Name string `json:"name"`
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &servers))
	require.Len(t, servers, 1)
	assert.Equal(t, deploymentName, servers[0].Name)
	assert.Equal(t, "remote", servers[0].Type)
	assert.Equal(
		t,
		fmt.Sprintf("http://%s.%s.svc.cluster.local:%d%s", serverName, kagentNamespace, kagentMCPPort, kagentMCPPath),
		servers[0].URL,
	)
}

func listKagentMCPTools(t *testing.T, workload string) []string {
	t.Helper()
	localPort := startKagentPortForward(t, workload)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d%s", localPort, kagentMCPPath)
	names := []string{}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		attemptCtx, attemptCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer attemptCancel()
		client := mcp.NewClient(&mcp.Implementation{Name: "agentregistry-e2e", Version: "0.0.0"}, nil)
		session, err := client.Connect(attemptCtx, &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
		if !assert.NoError(c, err) {
			return
		}
		defer func() { _ = session.Close() }()
		result, err := session.ListTools(attemptCtx, nil)
		if !assert.NoError(c, err) {
			return
		}
		names = names[:0]
		for _, tool := range result.Tools {
			names = append(names, tool.Name)
		}
		assert.NotEmpty(c, names)
	}, 2*time.Minute, 3*time.Second)
	return names
}

type portForwardResult struct {
	err    error
	output string
}

func startKagentPortForward(t *testing.T, workload string) int {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(
		ctx,
		"kubectl",
		"--context", e2eKubeContext,
		"--namespace", kagentNamespace,
		"port-forward",
		"--address", "127.0.0.1",
		"deployment/"+workload,
		fmt.Sprintf(":%d", kagentMCPPort),
	)
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	ready := make(chan int, 1)
	done := make(chan portForwardResult, 1)
	go func() {
		var stdoutText strings.Builder
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			stdoutText.WriteString(line)
			stdoutText.WriteByte('\n')
			var localPort int
			if _, scanErr := fmt.Sscanf(line, "Forwarding from 127.0.0.1:%d ->", &localPort); scanErr == nil {
				select {
				case ready <- localPort:
				default:
				}
			}
		}
		waitErr := cmd.Wait()
		if scanErr := scanner.Err(); waitErr == nil {
			waitErr = scanErr
		}
		done <- portForwardResult{err: waitErr, output: stdoutText.String() + stderr.String()}
	}()
	select {
	case localPort := <-ready:
		t.Cleanup(func() {
			cancel()
			<-done
		})
		return localPort
	case result := <-done:
		t.Fatalf("start Kagent MCP port-forward: %v\n%s", result.err, result.output)
	case <-time.After(30 * time.Second):
		cancel()
		result := <-done
		t.Fatalf("Kagent MCP port-forward did not become ready: %v\n%s", result.err, result.output)
	}
	return 0
}

func registerKagentRegistryCleanup(
	t *testing.T,
	workDir string,
	registryURL string,
	names kagentLifecycleNames,
) func() {
	t.Helper()
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			resources := [][]string{
				{"delete", "deployment", names.AgentDeploy},
				{"delete", "deployment", names.MCPDeployment},
				{"delete", "agent", names.Agent},
				{"delete", "mcpserver", names.MCPServer},
				{"delete", "model", names.Model, "--tag", "e2e"},
				{"delete", "-f", filepath.Join(workDir, "runtime.yaml")},
			}
			for _, arguments := range resources {
				arguments = append(arguments, "--registry-url", registryURL)
				checkKagentCleanupResult(t, RunArctl(t, workDir, arguments...))
			}
		})
	}
	t.Cleanup(cleanup)
	return cleanup
}

func checkKagentCleanupResult(t *testing.T, result ArctlResult) {
	t.Helper()
	if result.ExitCode == 0 {
		return
	}
	output := strings.ToLower(result.Stdout + "\n" + result.Stderr)
	if strings.Contains(output, "not found") {
		return
	}
	t.Errorf(
		"Kagent cleanup failed with exit code %d\nstdout: %s\nstderr: %s",
		result.ExitCode,
		result.Stdout,
		result.Stderr,
	)
}
