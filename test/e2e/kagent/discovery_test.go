//go:build e2e

package kagent

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	registryv1alpha1 "github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	e2e "github.com/agentregistry-dev/agentregistry/test/e2e"
)

func TestKagentDiscovery(t *testing.T) {
	env := newKagentTestEnvironment(t)
	id := e2e.UniqueNameWithPrefix("kagent-discovery")
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
