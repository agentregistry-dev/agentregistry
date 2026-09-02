//go:build e2e

package kagent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	e2e "github.com/agentregistry-dev/agentregistry/test/e2e"
)

func TestKagentAgentMCP(t *testing.T) {
	registryURL := registryBaseURL(t)
	workDir := t.TempDir()
	id := e2e.UniqueNameWithPrefix("kagent")
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

	e2e.RequireSuccess(t, e2e.RunArctl(t, workDir, "apply", "-f", manifestPath, "--registry-url", registryURL))
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
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n \"${KAGENT_NAMESPACE}\" wait --for=delete agent/" + agentName + " --timeout=2m")
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n \"${KAGENT_NAMESPACE}\" wait --for=delete mcpserver/" + mcpName + " --timeout=2m")
	docs.Command("arctl delete agent " + agentName)
	docs.Command("arctl delete mcpserver " + mcpName)
	docs.Command("arctl delete model " + modelName + " --tag e2e")
	docs.Command("arctl delete -f - <<EOF\n" + kagentRuntimeManifest(secretName, runtimeName) + "\nEOF")
	e2e.RequireSuccess(t, e2e.RunArctl(t, workDir, "delete", "deployment", agentDeploymentName, "--registry-url", registryURL))
	e2e.RequireSuccess(t, e2e.RunArctl(t, workDir, "delete", "deployment", mcpDeploymentName, "--registry-url", registryURL))
	waitForKagentResourceDeleted(t, "agents.kagent.dev", agentName)
	waitForKagentResourceDeleted(t, "mcpservers.kagent.dev", mcpName)
	waitForKagentWorkloadDeleted(t, agentName)
	waitForKagentWorkloadDeleted(t, mcpName)
}
