//go:build e2e

package kagent

import (
	"path/filepath"
	"testing"
	"time"

	e2e "github.com/agentregistry-dev/agentregistry/test/e2e"
)

func TestKagentAgent(t *testing.T) {
	env := newKagentTestEnvironment(t)
	id := e2e.UniqueNameWithPrefix("kagent-agent")
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
	docs.Command("kubectl --context kind-${KIND_CLUSTER_NAME} -n \"${KAGENT_NAMESPACE}\" wait --for=delete agent/" + agentName + " --timeout=2m")
	docs.Command("arctl delete agent " + agentName)
	docs.Command("arctl delete model " + modelName + " --tag e2e")
	docs.Command("arctl delete -f - <<EOF\n" + kagentRuntimeManifest(secretName, runtimeName) + "\nEOF")
	env.DeleteDeployment(deploymentName)
	waitForKagentResourceDeleted(t, "agents.kagent.dev", agentName)
	waitForKagentWorkloadDeleted(t, agentName)
}
