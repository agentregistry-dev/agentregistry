package kagent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

type fakeSecretResolver struct {
	ref   v1alpha1.SecretRef
	value secret.SensitiveValue
}

func (r *fakeSecretResolver) Resolve(
	_ context.Context,
	ref v1alpha1.SecretRef,
) (secret.SensitiveValue, error) {
	r.ref = ref
	return r.value, nil
}

func (*fakeSecretResolver) ResolveAll(
	context.Context,
	v1alpha1.SecretRef,
) (map[string]secret.SensitiveValue, error) {
	return nil, nil
}

func testAdapter(client *fakeClient, options ...Option) types.DeploymentAdapter {
	options = append(options, withClientFactory(func(runtimeConfig) (kagentClient, error) {
		return client, nil
	}))
	return New(options...)
}

func withRuntimeConfig(input types.ApplyInput) types.ApplyInput {
	input.Runtime.Spec.Config = map[string]any{
		"kagentUrl": "http://kagent:8083",
		"namespace": "kagent",
	}
	return input
}

func mcpApplyInput() types.ApplyInput {
	return types.ApplyInput{
		Deployment: &v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Name: "my-deploy", Namespace: "default"},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "gh-mcp"},
			},
		},
		Target: &v1alpha1.MCPServer{
			Metadata: v1alpha1.ObjectMeta{Name: "gh-mcp", Namespace: "default"},
			Spec: v1alpha1.MCPServerSpec{
				Remote: &v1alpha1.MCPRemote{
					Type: "streamable-http",
					URL:  "https://mcp.example.com/mcp",
				},
			},
		},
		Runtime: &v1alpha1.Runtime{Spec: v1alpha1.RuntimeSpec{Type: RuntimeType}},
	}
}

func resultRuntimeMetadata(t *testing.T, result *types.ApplyResult) map[string]string {
	t.Helper()
	var metadata map[string]string
	require.NoError(t, json.Unmarshal(result.Details[runtimeDetailsKey], &metadata))
	return metadata
}

func findCondition(
	conditions []v1alpha1.Condition,
	conditionType string,
) *v1alpha1.Condition {
	for i := range conditions {
		if conditions[i].Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

func TestApplyAgentUsesEstablishedRuntimeMetadata(t *testing.T) {
	client := newFakeClient()
	input := withRuntimeConfig(byoApplyInput())

	result, err := testAdapter(client).Apply(context.Background(), input)
	require.NoError(t, err)
	require.Contains(t, client.agents, "kagent/my-agent")
	assert.Equal(t, map[string]string{
		"remoteId":   "my-agent",
		"remoteName": "my-agent",
		"namespace":  "kagent",
		"image":      "ghcr.io/acme/agent:1.0.0",
	}, resultRuntimeMetadata(t, result))
	assert.Equal(t, map[string]string{
		runtimeRemoteIDAnnotationKey: "my-agent",
	}, result.RuntimeMetadata)
	assert.Equal(t, v1alpha1.ConditionTrue, findCondition(result.Conditions, "Ready").Status)
}

func TestApplyAndDiscoverUseSameRemoteIDForNormalizedAgentName(t *testing.T) {
	client := newFakeClient()
	adapter := testAdapter(client)
	input := withRuntimeConfig(byoApplyInput())

	result, err := adapter.Apply(context.Background(), input)
	require.NoError(t, err)
	discovered, err := adapter.(types.DeploymentDiscoverySource).Discover(
		context.Background(),
		types.DiscoverInput{Runtime: input.Runtime},
	)
	require.NoError(t, err)
	require.Len(t, discovered, 1)
	assert.Equal(t, "my-agent", result.RuntimeMetadata[runtimeRemoteIDAnnotationKey])
	assert.Equal(t, "my-agent", discovered[0].RuntimeMetadata[types.RuntimeMetadataRemoteIDKey])
}

func TestDesiredFingerprintWaitsForAutomaticMCPDeploymentEndpoint(t *testing.T) {
	input := withRuntimeConfig(byoApplyInput())
	input.Deployment.Spec.RuntimeRef = v1alpha1.ResourceRef{
		Kind: v1alpha1.KindRuntime,
		Name: "kagent-prod",
	}
	input.Runtime.Metadata = v1alpha1.ObjectMeta{Name: "kagent-prod", Namespace: "default"}
	input.Target.(*v1alpha1.Agent).TypeMeta = v1alpha1.TypeMeta{
		APIVersion: v1alpha1.GroupVersion,
		Kind:       v1alpha1.KindAgent,
	}
	input.Target.(*v1alpha1.Agent).Spec.MCPServers = []v1alpha1.ResourceRef{{Name: "tools"}}
	input.Getter = func(context.Context, v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return &v1alpha1.MCPServer{
			TypeMeta: v1alpha1.TypeMeta{
				APIVersion: v1alpha1.GroupVersion,
				Kind:       v1alpha1.KindMCPServer,
			},
			Metadata: v1alpha1.ObjectMeta{Name: "tools", Namespace: "default"},
			Spec:     v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{}},
		}, nil
	}
	mcpDeployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Name: "tools-prod", Namespace: "default"},
	}
	finder := DeploymentFinderFunc(func(
		context.Context,
		v1alpha1.ResourceRef,
		v1alpha1.ResourceRef,
	) (*v1alpha1.Deployment, bool, error) {
		return mcpDeployment, true, nil
	})

	fingerprinter := New(WithDeploymentFinder(finder)).(types.DeploymentDesiredFingerprinter)
	_, err := fingerprinter.DesiredFingerprint(context.Background(), input)
	require.ErrorIs(t, err, ErrDependencyNotReady)
	result, err := New(WithDeploymentFinder(finder)).Apply(context.Background(), input)
	require.ErrorIs(t, err, ErrDependencyNotReady)
	require.Nil(t, result)
	mcpDeployment.Status.Conditions = []v1alpha1.Condition{{
		Type:    mcpServerURLCondition,
		Status:  v1alpha1.ConditionTrue,
		Message: "http://tools.kagent.svc.cluster.local:3000/mcp",
	}}
	fingerprint, err := fingerprinter.DesiredFingerprint(context.Background(), input)
	require.NoError(t, err)
	assert.NotEmpty(t, fingerprint)
}

func TestApplyMCPServerUsesEstablishedRuntimeMetadata(t *testing.T) {
	client := newFakeClient()
	input := withRuntimeConfig(mcpApplyInput())

	result, err := testAdapter(client).Apply(context.Background(), input)
	require.NoError(t, err)
	require.Contains(t, client.toolServers, "kagent/gh-mcp")
	assert.Equal(t, map[string]string{
		"remoteId":   "gh-mcp",
		"remoteName": "gh-mcp",
		"namespace":  "kagent",
		"kind":       "RemoteMCPServer",
	}, resultRuntimeMetadata(t, result))
	assert.Equal(t, map[string]string{
		runtimeRemoteIDAnnotationKey: "gh-mcp",
	}, result.RuntimeMetadata)
	endpoint := findCondition(result.Conditions, mcpServerURLCondition)
	require.NotNil(t, endpoint)
	assert.Equal(t, v1alpha1.ConditionTrue, endpoint.Status)
	assert.Equal(t, "https://mcp.example.com/mcp", endpoint.Message)
}

func TestApplyAddsConfiguredLabelsToPodBackedResources(t *testing.T) {
	client := newFakeClient()
	input := withRuntimeConfig(byoApplyInput())
	adapter := testAdapter(client, WithWorkloadLabels(map[string]string{"example.com/managed": "true"}))

	_, err := adapter.Apply(context.Background(), input)
	require.NoError(t, err)
	agent := client.agents["kagent/my-agent"]
	assert.Equal(t, "true", agent.Labels["example.com/managed"])
	assert.Equal(t, "true", agent.Spec.BYO.Deployment.Labels["example.com/managed"])
}

func TestApplyUnsupportedHarnessReturnsFailedStatus(t *testing.T) {
	input := withRuntimeConfig(byoApplyInput())
	input.Deployment.Spec.Harness = &v1alpha1.DeploymentHarness{Type: "claude-code"}

	result, err := testAdapter(newFakeClient()).Apply(context.Background(), input)
	require.NoError(t, err)
	ready := findCondition(result.Conditions, "Ready")
	require.NotNil(t, ready)
	assert.Equal(t, "Failed", ready.Reason)
	assert.Nil(t, findCondition(result.Conditions, "Degraded"))
}

func TestApplyInvalidRuntimeConfigReturnsFailedStatus(t *testing.T) {
	input := byoApplyInput()
	input.Runtime.Spec.Config = map[string]any{"namespace": "kagent"}

	result, err := testAdapter(newFakeClient()).Apply(context.Background(), input)
	require.NoError(t, err)
	ready := findCondition(result.Conditions, "Ready")
	require.NotNil(t, ready)
	assert.Equal(t, "Failed", ready.Reason)
	assert.Nil(t, findCondition(result.Conditions, "Degraded"))
}

func TestApplyPropagatesKagentError(t *testing.T) {
	client := newFakeClient()
	client.errs["ensureAgent:kagent/my-agent"] = assert.AnError

	_, err := testAdapter(client).Apply(context.Background(), withRuntimeConfig(byoApplyInput()))
	assert.ErrorIs(t, err, assert.AnError)
}

func removeInput(metadata map[string]string) types.RemoveInput {
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Name: "my-deploy"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: "My Agent"},
		},
	}
	if metadata != nil {
		if err := deployment.Status.SetDetailsKey(runtimeDetailsKey, metadata); err != nil {
			panic(err)
		}
	}
	return types.RemoveInput{
		Deployment: deployment,
		Runtime: &v1alpha1.Runtime{Spec: v1alpha1.RuntimeSpec{
			Type: RuntimeType,
			Config: map[string]any{
				"kagentUrl": "http://kagent:8083",
				"namespace": "kagent",
			},
		}},
	}
}

func TestRemoveUsesRecordedIdentity(t *testing.T) {
	client := newFakeClient()
	result, err := testAdapter(client).Remove(context.Background(), removeInput(map[string]string{
		"remoteId":  "existing-agent",
		"namespace": "team-a",
	}))
	require.NoError(t, err)
	assert.Equal(t, []string{"Agent:team-a/existing-agent"}, client.deleted)
	assert.Equal(t, "Removed", findCondition(result.Conditions, "Ready").Reason)
}

func TestRemoveDerivesIdentityWhenDeploymentWasNotMaterialized(t *testing.T) {
	client := newFakeClient()
	_, err := testAdapter(client).Remove(context.Background(), removeInput(nil))
	require.NoError(t, err)
	assert.Equal(t, []string{"Agent:kagent/my-agent"}, client.deleted)
}

func TestRemoveTreatsMissingRuntimeResourceAsSuccess(t *testing.T) {
	client := newFakeClient()
	client.errs["deleteAgent:kagent/my-agent"] = errNotFound
	_, err := testAdapter(client).Remove(context.Background(), removeInput(nil))
	assert.NoError(t, err)
}

func TestRemoveMCPServerClearsEndpoint(t *testing.T) {
	client := newFakeClient()
	input := removeInput(map[string]string{"remoteId": "tools", "namespace": "kagent"})
	input.Deployment.Spec.TargetRef.Kind = v1alpha1.KindMCPServer

	result, err := testAdapter(client).Remove(context.Background(), input)
	require.NoError(t, err)
	condition := findCondition(result.Conditions, mcpServerURLCondition)
	require.NotNil(t, condition)
	assert.Equal(t, v1alpha1.ConditionFalse, condition.Status)
	assert.Equal(t, "Removed", condition.Reason)
}

func TestToolServerEndpointUsesDeclaredPath(t *testing.T) {
	server := &toolServerSpec{MCP: &mcpServerPayload{}}
	server.MCP.Name = "tools"
	server.MCP.Namespace = "kagent"
	server.MCP.Spec.Deployment.Port = 8080
	server.MCP.Spec.HTTPTransport = &httpTransportPayload{TargetPath: "/upstream-mcp"}

	assert.Equal(
		t,
		"http://tools.kagent.svc.cluster.local:8080/upstream-mcp",
		toolServerEndpoint(server),
	)
}

func TestLogsReportsUnsupported(t *testing.T) {
	_, err := testAdapter(newFakeClient()).Logs(context.Background(), types.LogsInput{})
	require.ErrorContains(t, err, "not supported")
}

func TestSecretResolverBuildsRuntimeTokenSource(t *testing.T) {
	resolver := &fakeSecretResolver{value: secret.NewSensitiveValue([]byte("secret-token"))}
	a := New(WithSecretResolver(resolver)).(*adapter)
	runtime := &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Name: "runtime-one", Namespace: "team-a"},
	}
	tokenSource, err := a.tokenSourceFor(context.Background(), runtime, authConfig{
		SecretRef: &v1alpha1.SecretRef{Name: "kagent-credentials", Key: "token"},
	})
	require.NoError(t, err)
	token, err := tokenSource.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "secret-token", token)
	assert.Equal(t, v1alpha1.SecretRef{
		Namespace: "team-a",
		Name:      "kagent-credentials",
		Key:       "token",
	}, resolver.ref)
}

func TestTokenSourceFactoryReceivesRuntime(t *testing.T) {
	var received *v1alpha1.Runtime
	a := New(WithTokenSourceFactory(func(
		_ context.Context,
		runtime *v1alpha1.Runtime,
	) (TokenSource, error) {
		received = runtime
		return StaticToken("runtime-token"), nil
	})).(*adapter)
	runtime := &v1alpha1.Runtime{Metadata: v1alpha1.ObjectMeta{Name: "runtime-one"}}

	tokenSource, err := a.tokenSourceFor(context.Background(), runtime, authConfig{})
	require.NoError(t, err)
	assert.Same(t, runtime, received)
	token, err := tokenSource.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "runtime-token", token)
}

func TestRemovePropagatesDeleteFailure(t *testing.T) {
	client := newFakeClient()
	client.errs["deleteAgent:kagent/my-agent"] = errors.New("delete failed")
	_, err := testAdapter(client).Remove(context.Background(), removeInput(nil))
	require.ErrorContains(t, err, "delete failed")
}
