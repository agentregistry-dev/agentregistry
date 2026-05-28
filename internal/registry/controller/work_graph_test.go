package controller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestDeploymentWorkIntentTracksRuntimeDependency(t *testing.T) {
	sources := newWorkGraphSourceIndex()
	intents := NewDeploymentWorkIntents(sources)

	deployment := deploymentFixture("")
	sources.MCPServers.UpdateObject(MCPServerSource{
		Key:       sourceKey(v1alpha1.KindMCPServer, "weather", "stable"),
		MCPServer: mcpServerFixture("weather", "stable", 1),
	})
	sources.Deployments.UpdateObject(DeploymentSource{
		Key:        sourceKey(v1alpha1.KindDeployment, "weather", ""),
		Deployment: deployment,
	})

	intent := waitForDeploymentWorkIntent(t, intents, "Deployment:default:weather:uid-1:7:apply")
	require.Equal(t, "runtime-reference-pending", intent.Work.Reason)
	require.Contains(t, intent.Dependencies, DeploymentWorkDependency{
		Role:    dependencyRoleRuntime,
		Key:     sourceKey(v1alpha1.KindRuntime, "local", ""),
		Missing: true,
	})

	sources.Runtimes.UpdateObject(RuntimeSource{
		Key:     sourceKey(v1alpha1.KindRuntime, "local", ""),
		Runtime: runtimeFixture("local", 1),
	})

	require.Eventually(t, func() bool {
		intent = waitForDeploymentWorkIntent(t, intents, "Deployment:default:weather:uid-1:7:apply")
		return intent.Work.Reason == "desired-deployed" &&
			intentDependency(intent, dependencyRoleRuntime, v1alpha1.KindRuntime, "local").Generation == 1
	}, time.Second, 10*time.Millisecond)
}

func TestDeploymentWorkIntentTracksAgentMCPServerDependencies(t *testing.T) {
	sources := newWorkGraphSourceIndex()
	intents := NewDeploymentWorkIntents(sources)

	deployment := deploymentFixture("")
	deployment.Spec.TargetRef = v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: "chat", Tag: "v1"}

	sources.Runtimes.UpdateObject(RuntimeSource{
		Key:     sourceKey(v1alpha1.KindRuntime, "local", ""),
		Runtime: runtimeFixture("local", 1),
	})
	sources.MCPServers.UpdateObject(MCPServerSource{
		Key:       sourceKey(v1alpha1.KindMCPServer, "tools", "v1"),
		MCPServer: mcpServerFixture("tools", "v1", 1),
	})
	sources.Agents.UpdateObject(AgentSource{
		Key: sourceKey(v1alpha1.KindAgent, "chat", "v1"),
		Agent: agentFixture("chat", "v1", 1, []v1alpha1.ResourceRef{{
			Kind: v1alpha1.KindMCPServer,
			Name: "tools",
			Tag:  "v1",
		}}),
	})
	sources.Deployments.UpdateObject(DeploymentSource{
		Key:        sourceKey(v1alpha1.KindDeployment, "weather", ""),
		Deployment: deployment,
	})

	intent := waitForDeploymentWorkIntent(t, intents, "Deployment:default:weather:uid-1:7:apply")
	require.Equal(t, int64(1), intentDependency(intent, dependencyRoleAgentMCPServer, v1alpha1.KindMCPServer, "tools").Generation)

	sources.MCPServers.UpdateObject(MCPServerSource{
		Key:       sourceKey(v1alpha1.KindMCPServer, "tools", "v1"),
		MCPServer: mcpServerFixture("tools", "v1", 2),
	})

	require.Eventually(t, func() bool {
		intent = waitForDeploymentWorkIntent(t, intents, "Deployment:default:weather:uid-1:7:apply")
		return intentDependency(intent, dependencyRoleAgentMCPServer, v1alpha1.KindMCPServer, "tools").Generation == 2
	}, time.Second, 10*time.Millisecond)
}

func TestDeploymentWorkIntentSyncsExistingSourceState(t *testing.T) {
	sources := newWorkGraphSourceIndex()
	sources.Runtimes.UpdateObject(RuntimeSource{
		Key:     sourceKey(v1alpha1.KindRuntime, "local", ""),
		Runtime: runtimeFixture("local", 1),
	})
	sources.MCPServers.UpdateObject(MCPServerSource{
		Key:       sourceKey(v1alpha1.KindMCPServer, "weather", "stable"),
		MCPServer: mcpServerFixture("weather", "stable", 1),
	})
	sources.Deployments.UpdateObject(DeploymentSource{
		Key:        sourceKey(v1alpha1.KindDeployment, "weather", ""),
		Deployment: deploymentFixture(""),
	})

	intents := NewDeploymentWorkIntents(sources)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.True(t, intents.WaitUntilSynced(ctx.Done()))

	intent := intents.GetKey("Deployment:default:weather:uid-1:7:apply")
	require.NotNil(t, intent)
	require.Equal(t, "desired-deployed", intent.Work.Reason)
}

func newWorkGraphSourceIndex() *SourceIndex {
	return NewSourceIndex(map[string]*v1alpha1store.Store{
		v1alpha1.KindDeployment: nil,
		v1alpha1.KindRuntime:    nil,
		v1alpha1.KindAgent:      nil,
		v1alpha1.KindMCPServer:  nil,
	})
}

func waitForDeploymentWorkIntent(
	t *testing.T,
	intents interface {
		GetKey(string) *DeploymentWorkIntent
	},
	key string,
) DeploymentWorkIntent {
	t.Helper()
	var intent *DeploymentWorkIntent
	require.Eventually(t, func() bool {
		intent = intents.GetKey(key)
		return intent != nil
	}, time.Second, 10*time.Millisecond)
	return *intent
}

func intentDependency(intent DeploymentWorkIntent, role, kind, name string) DeploymentWorkDependency {
	for _, dependency := range intent.Dependencies {
		if dependency.Role == role && dependency.Key.Kind == kind && dependency.Key.Name == name {
			return dependency
		}
	}
	return DeploymentWorkDependency{}
}

func runtimeFixture(name string, generation int64) *v1alpha1.Runtime {
	return &v1alpha1.Runtime{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindRuntime},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  v1alpha1.DefaultNamespace,
			Name:       name,
			UID:        name + "-uid",
			Generation: generation,
		},
		Spec: v1alpha1.RuntimeSpec{Type: v1alpha1.TypeLocal},
	}
}

func agentFixture(name, tag string, generation int64, mcpServers []v1alpha1.ResourceRef) *v1alpha1.Agent {
	return &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  v1alpha1.DefaultNamespace,
			Name:       name,
			Tag:        tag,
			UID:        name + "-uid",
			Generation: generation,
		},
		Spec: v1alpha1.AgentSpec{MCPServers: mcpServers},
	}
}

func mcpServerFixture(name, tag string, generation int64) *v1alpha1.MCPServer {
	return &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  v1alpha1.DefaultNamespace,
			Name:       name,
			Tag:        tag,
			UID:        name + "-uid",
			Generation: generation,
		},
		Spec: v1alpha1.MCPServerSpec{Title: name},
	}
}

func sourceKey(kind, name, tag string) v1alpha1store.ResourceKey {
	return v1alpha1store.ResourceKey{
		Kind:      kind,
		Namespace: v1alpha1.DefaultNamespace,
		Name:      name,
		Tag:       tag,
	}
}
