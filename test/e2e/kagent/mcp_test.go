//go:build e2e

package kagent

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	e2e "github.com/agentregistry-dev/agentregistry/test/e2e"
)

func TestKagentMCP(t *testing.T) {
	env := newKagentTestEnvironment(t)
	id := e2e.UniqueNameWithPrefix("kagent-mcp")
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
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n \"${KAGENT_NAMESPACE}\" wait --for=delete remotemcpserver/" + remoteName + " --timeout=2m")
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n \"${KAGENT_NAMESPACE}\" wait --for=delete mcpserver/" + sourceName + " --timeout=2m")
	docs.Command("arctl delete mcpserver " + remoteName)
	docs.Command("arctl delete mcpserver " + sourceName)
	docs.Command("arctl delete -f - <<EOF\n" + kagentRuntimeManifest(secretName, runtimeName) + "\nEOF")
	env.DeleteDeployment(remoteDeploymentName)
	env.DeleteDeployment(sourceDeploymentName)
	waitForKagentResourceDeleted(t, "remotemcpservers.kagent.dev", remoteName)
	waitForKagentResourceDeleted(t, "mcpservers.kagent.dev", sourceName)
	waitForKagentWorkloadDeleted(t, sourceName)
}
