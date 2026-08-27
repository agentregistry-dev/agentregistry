package kagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func byoApplyInput() types.ApplyInput {
	return types.ApplyInput{
		Deployment: &v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Name: "my-deploy", Namespace: "default"},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: "My Agent"},
				Env:       map[string]string{"FOO": "bar"},
			},
		},
		Target: &v1alpha1.Agent{
			Metadata: v1alpha1.ObjectMeta{Name: "My Agent", Namespace: "default"},
			Spec: v1alpha1.AgentSpec{
				Source: &v1alpha1.AgentSource{Image: "ghcr.io/acme/agent:1.0.0"},
			},
		},
		Runtime: &v1alpha1.Runtime{
			Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, TelemetryEndpoint: "http://otel:4317"},
		},
	}
}

func buildTestBYOAgent(
	ctx context.Context,
	in types.ApplyInput,
	runtimeConfig runtimeConfig,
) (*agentPayload, error) {
	return buildBYOAgent(ctx, in, runtimeConfig, nil)
}

func TestWorkloadName(t *testing.T) {
	assert.Equal(t, "my-agent", WorkloadName("My Agent"))
	assert.Equal(t, WorkloadName("a_b.c"), WorkloadName("a-b-c"))
}

func TestWorkloadNameMatchesKagentResourceNaming(t *testing.T) {
	assert.Equal(t, "my-agent-v1", WorkloadName(" My_Agent@v1 "))
	assert.Equal(t, "foo-bar", WorkloadName("foo--bar"))
	assert.Equal(t, strings.Repeat("a", 80), WorkloadName(strings.Repeat("A", 80)))
}

func TestBuildBYOAgent(t *testing.T) {
	in := byoApplyInput()
	workloadName := WorkloadName(in.Target.GetMetadata().Name)
	got, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{
		URL:              "https://kagent.example.com",
		Namespace:        "kagent",
		ImagePullSecrets: []string{"registry-creds"},
		Deployment: runtimeDeploymentConfig{
			NodeSelector: map[string]string{"node-group": "agents"},
			Tolerations: []corev1.Toleration{{
				Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "agents", Effect: corev1.TaintEffectNoSchedule,
			}},
			Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
		},
	})
	require.NoError(t, err)
	want := &agentPayload{}
	want.Name = workloadName
	want.Namespace = "kagent"
	want.Spec = agentPayloadSpec{
		Type:        agentTypeBYO,
		Description: workloadName,
		BYO: &byoAgentPayloadSpec{
			Deployment: &byoDeploymentPayload{
				Image:            "ghcr.io/acme/agent:1.0.0",
				ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-creds"}},
				NodeSelector:     map[string]string{"node-group": "agents"},
				Tolerations: []corev1.Toleration{{
					Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "agents", Effect: corev1.TaintEffectNoSchedule,
				}},
				Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
				Env: []corev1.EnvVar{
					{Name: "FOO", Value: "bar"},
					{Name: "HOST", Value: "0.0.0.0"},
					{Name: "KAGENT_NAMESPACE", Value: "kagent"},
					{Name: "KAGENT_NAME", Value: "My Agent"},
					{Name: "KAGENT_URL", Value: "https://kagent.example.com"},
				},
			},
		},
	}
	assert.Equal(t, want, got)
}

func TestBuildBYOAgentDefaultsWorkloadNamespaceToKagent(t *testing.T) {
	in := byoApplyInput()

	got, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{})
	require.NoError(t, err)
	assert.Equal(t, "kagent", got.Namespace)
	assertEnvValueOnce(t, got.Spec.BYO.Deployment.Env, "KAGENT_NAMESPACE", "kagent")
}

func TestBuildBYOAgentUsesRuntimeNamespace(t *testing.T) {
	in := byoApplyInput()

	got, err := buildTestBYOAgent(
		context.Background(),
		in,
		runtimeConfig{Namespace: "runtime-namespace"},
	)
	require.NoError(t, err)
	assert.Equal(t, "runtime-namespace", got.Namespace)
}

func TestBuildBYOAgentUsesTargetNameForWorkloadIdentity(t *testing.T) {
	in := byoApplyInput()
	in.Deployment.Metadata.Name = "Team Agent Production"

	got, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{})
	require.NoError(t, err)
	assert.Equal(t, WorkloadName(in.Target.GetMetadata().Name), got.Name)
}

func TestBuildBYOAgentResolvesDeploymentModelRef(t *testing.T) {
	in := byoApplyInput()
	in.Deployment.Spec.ModelRef = &v1alpha1.ModelRef{
		Namespace: "models",
		Name:      "claude",
		Tag:       "approved",
	}
	var gotRef v1alpha1.ResourceRef
	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		gotRef = ref
		return &v1alpha1.Model{Spec: v1alpha1.ModelSpec{
			Provider: "bedrock",
			Model:    "us.anthropic.claude-sonnet-4-6",
		}}, nil
	}

	got, err := buildTestBYOAgent(
		context.Background(),
		in,
		runtimeConfig{Namespace: "kagent"},
	)
	require.NoError(t, err)
	assert.Equal(t, v1alpha1.ResourceRef{
		Kind:      v1alpha1.KindModel,
		Namespace: "models",
		Name:      "claude",
		Tag:       "approved",
	}, gotRef)
	assert.Contains(t, got.Spec.BYO.Deployment.Env, corev1.EnvVar{Name: "MODEL_PROVIDER", Value: "bedrock"})
	assert.Contains(t, got.Spec.BYO.Deployment.Env, corev1.EnvVar{
		Name:  "MODEL_NAME",
		Value: "us.anthropic.claude-sonnet-4-6",
	})
}

func TestBuildBYOAgentPreservesDeploymentModelEnvWithoutModelRef(t *testing.T) {
	in := byoApplyInput()
	in.Deployment.Spec.Env["MODEL_PROVIDER"] = "deployment-provider"
	in.Deployment.Spec.Env["MODEL_NAME"] = "deployment-model"

	got, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{})
	require.NoError(t, err)
	assertEnvValueOnce(t, got.Spec.BYO.Deployment.Env, "MODEL_PROVIDER", "deployment-provider")
	assertEnvValueOnce(t, got.Spec.BYO.Deployment.Env, "MODEL_NAME", "deployment-model")
}

func TestBuildBYOAgentResolvesRemoteAndDeployedMCPServers(t *testing.T) {
	in := byoApplyInput()
	agent := in.Target.(*v1alpha1.Agent)
	agent.Spec.MCPServers = []v1alpha1.ResourceRef{
		{Name: "remote-tools"},
		{Name: "local-tools"},
	}
	in.Deployment.Spec.DeploymentRefs = []v1alpha1.DeploymentRef{{Name: "local-tools-prod"}}
	in.Deployment.Spec.RuntimeRef = v1alpha1.ResourceRef{Name: "kagent-prod"}
	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		switch ref.Kind + "/" + ref.Name {
		case v1alpha1.KindMCPServer + "/remote-tools":
			return &v1alpha1.MCPServer{
				Metadata: v1alpha1.ObjectMeta{Name: "remote-tools", Namespace: "default"},
				Spec: v1alpha1.MCPServerSpec{Remote: &v1alpha1.MCPRemote{
					URL:     "https://tools.example.com/mcp",
					Headers: []v1alpha1.HTTPHeader{{Name: "Authorization", Value: "Bearer token"}},
				}},
			}, nil
		case v1alpha1.KindMCPServer + "/local-tools":
			return &v1alpha1.MCPServer{
				Metadata: v1alpha1.ObjectMeta{Name: "local-tools", Namespace: "default"},
				Spec:     v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{}},
			}, nil
		case v1alpha1.KindDeployment + "/local-tools-prod":
			return &v1alpha1.Deployment{
				Metadata: v1alpha1.ObjectMeta{Name: "local-tools-prod", Namespace: "default"},
				Spec: v1alpha1.DeploymentSpec{
					TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "local-tools"},
					RuntimeRef: v1alpha1.ResourceRef{Name: "kagent-prod"},
				},
				Status: v1alpha1.Status{Conditions: []v1alpha1.Condition{{
					Type: mcpServerURLCondition, Status: v1alpha1.ConditionTrue,
					Message: "http://local-tools.kagent.svc.cluster.local:3000/mcp",
				}}},
			}, nil
		default:
			t.Fatalf("unexpected ref: %+v", ref)
			return nil, nil
		}
	}

	got, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{Namespace: "kagent"})
	require.NoError(t, err)
	var configs []mcpRuntimeConfig
	require.NoError(t, json.Unmarshal([]byte(envValue(t, got.Spec.BYO.Deployment.Env, mcpServersConfigEnv)), &configs))
	assert.Equal(t, []mcpRuntimeConfig{
		{
			Name: "remote-tools", Type: "remote", URL: "https://tools.example.com/mcp",
			Headers: map[string]string{"Authorization": "Bearer token"},
		},
		{
			Name: "local-tools-prod", Type: "remote",
			URL: "http://local-tools.kagent.svc.cluster.local:3000/mcp",
		},
	}, configs)
}

func TestBuildBYOAgentFindsSourceBackedMCPDeployment(t *testing.T) {
	in := byoApplyInput()
	in.Target.(*v1alpha1.Agent).Spec.MCPServers = []v1alpha1.ResourceRef{{Name: "local-tools"}}
	in.Deployment.Spec.RuntimeRef = v1alpha1.ResourceRef{Name: "kagent-prod"}
	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		if ref.Kind != v1alpha1.KindMCPServer || ref.Namespace != "default" || ref.Name != "local-tools" {
			return nil, fmt.Errorf("unexpected ref: %+v", ref)
		}
		return &v1alpha1.MCPServer{
			Metadata: v1alpha1.ObjectMeta{Name: "local-tools", Namespace: "default"},
			Spec:     v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{}},
		}, nil
	}
	findDeployment := DeploymentFinderFunc(func(
		_ context.Context,
		targetRef v1alpha1.ResourceRef,
		runtimeRef v1alpha1.ResourceRef,
	) (*v1alpha1.Deployment, bool, error) {
		if targetRef.Kind != v1alpha1.KindMCPServer ||
			targetRef.Namespace != "default" ||
			targetRef.Name != "local-tools" ||
			runtimeRef.Namespace != "default" ||
			runtimeRef.Name != "kagent-prod" {
			return nil, false, fmt.Errorf(
				"unexpected deployment lookup: target=%+v runtime=%+v",
				targetRef,
				runtimeRef,
			)
		}
		return &v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Name: "local-tools-prod", Namespace: "default"},
			Status: v1alpha1.Status{Conditions: []v1alpha1.Condition{{
				Type:    mcpServerURLCondition,
				Status:  v1alpha1.ConditionTrue,
				Message: "http://local-tools.kagent.svc.cluster.local:3000/mcp",
			}}},
		}, true, nil
	})

	got, err := buildBYOAgent(
		context.Background(),
		in,
		runtimeConfig{},
		findDeployment,
	)
	require.NoError(t, err)
	var configs []mcpRuntimeConfig
	require.NoError(t, json.Unmarshal(
		[]byte(envValue(t, got.Spec.BYO.Deployment.Env, mcpServersConfigEnv)),
		&configs,
	))
	assert.Equal(t, []mcpRuntimeConfig{{
		Name: "local-tools-prod",
		Type: "remote",
		URL:  "http://local-tools.kagent.svc.cluster.local:3000/mcp",
	}}, configs)
}

func TestBuildBYOAgentWaitsForSourceBackedMCPDeploymentEndpoint(t *testing.T) {
	in := byoApplyInput()
	in.Target.(*v1alpha1.Agent).Spec.MCPServers = []v1alpha1.ResourceRef{{Name: "local-tools"}}
	in.Deployment.Spec.RuntimeRef = v1alpha1.ResourceRef{Name: "kagent-prod"}
	in.Getter = func(_ context.Context, _ v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return &v1alpha1.MCPServer{
			Metadata: v1alpha1.ObjectMeta{Name: "local-tools", Namespace: "default"},
			Spec:     v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{}},
		}, nil
	}
	findDeployment := DeploymentFinderFunc(func(
		context.Context,
		v1alpha1.ResourceRef,
		v1alpha1.ResourceRef,
	) (*v1alpha1.Deployment, bool, error) {
		return &v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Name: "local-tools-prod", Namespace: "default"},
		}, true, nil
	})

	_, err := buildBYOAgent(
		context.Background(),
		in,
		runtimeConfig{},
		findDeployment,
	)
	require.ErrorIs(t, err, errDependencyNotReady)
}

func TestBuildBYOAgentMatchesDeploymentRefRuntimeByName(t *testing.T) {
	in := byoApplyInput()
	in.Deployment.Metadata.Namespace = "agents"
	in.Deployment.Spec.RuntimeRef = v1alpha1.ResourceRef{Name: "kagent-prod"}
	in.Deployment.Spec.DeploymentRefs = []v1alpha1.DeploymentRef{{Namespace: "tools", Name: "tools-prod"}}
	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return &v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Name: ref.Name, Namespace: ref.Namespace},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "tools"},
				RuntimeRef: v1alpha1.ResourceRef{Name: "kagent-prod"},
			},
			Status: v1alpha1.Status{Conditions: []v1alpha1.Condition{{
				Type: mcpServerURLCondition, Status: v1alpha1.ConditionTrue, Message: "http://tools/mcp",
			}}},
		}, nil
	}
	got, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{})
	require.NoError(t, err)
	assert.NotEmpty(t, envValue(t, got.Spec.BYO.Deployment.Env, mcpServersConfigEnv))
}

func TestBuildBYOAgentMatchesMCPDeploymentByNamespaceAndName(t *testing.T) {
	in := byoApplyInput()
	in.Target.(*v1alpha1.Agent).Spec.MCPServers = []v1alpha1.ResourceRef{{Name: "tools", Tag: "v2"}}
	in.Deployment.Spec.DeploymentRefs = []v1alpha1.DeploymentRef{{Name: "tools-prod"}}
	in.Getter = func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		if ref.Kind == v1alpha1.KindMCPServer {
			return &v1alpha1.MCPServer{
				Metadata: v1alpha1.ObjectMeta{Name: "tools", Namespace: "default", Tag: "v2"},
				Spec:     v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{}},
			}, nil
		}
		return &v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Name: "tools-prod", Namespace: "default"},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "tools", Tag: "v1"},
			},
			Status: v1alpha1.Status{Conditions: []v1alpha1.Condition{{
				Type: mcpServerURLCondition, Status: v1alpha1.ConditionTrue, Message: "http://tools/mcp",
			}}},
		}, nil
	}
	findDeployment := DeploymentFinderFunc(func(
		context.Context,
		v1alpha1.ResourceRef,
		v1alpha1.ResourceRef,
	) (*v1alpha1.Deployment, bool, error) {
		return nil, false, fmt.Errorf("automatic lookup should not run for an explicitly resolved MCPServer")
	})

	got, err := buildBYOAgent(
		context.Background(),
		in,
		runtimeConfig{},
		findDeployment,
	)
	require.NoError(t, err)
	var configs []mcpRuntimeConfig
	require.NoError(t, json.Unmarshal(
		[]byte(envValue(t, got.Spec.BYO.Deployment.Env, mcpServersConfigEnv)),
		&configs,
	))
	assert.Equal(t, []mcpRuntimeConfig{{
		Name: "tools-prod",
		Type: "remote",
		URL:  "http://tools/mcp",
	}}, configs)
}

func TestBuildBYOAgentOverridesAdapterManagedEnvWithoutDuplicates(t *testing.T) {
	in := byoApplyInput()
	in.Deployment.Spec.Env = map[string]string{
		"HOST":                        "127.0.0.1",
		"KAGENT_NAMESPACE":            "wrong",
		"KAGENT_NAME":                 "wrong",
		"KAGENT_URL":                  "https://wrong.example.com",
		"MODEL_PROVIDER":              "wrong",
		"MODEL_NAME":                  "wrong",
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://wrong:4317",
	}
	in.Deployment.Spec.ModelRef = &v1alpha1.ModelRef{Name: "model"}
	in.Getter = func(_ context.Context, _ v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return &v1alpha1.Model{Spec: v1alpha1.ModelSpec{Provider: "openai", Model: "gpt-5"}}, nil
	}

	got, err := buildTestBYOAgent(
		context.Background(), in,
		runtimeConfig{URL: "https://kagent.example.com", Namespace: "kagent"},
	)
	require.NoError(t, err)
	env := got.Spec.BYO.Deployment.Env
	assertEnvValueOnce(t, env, "HOST", "127.0.0.1")
	assertEnvValueOnce(t, env, "KAGENT_NAMESPACE", "kagent")
	assertEnvValueOnce(t, env, "KAGENT_NAME", in.Target.GetMetadata().Name)
	assertEnvValueOnce(t, env, "KAGENT_URL", "https://kagent.example.com")
	assertEnvValueOnce(t, env, "MODEL_PROVIDER", "openai")
	assertEnvValueOnce(t, env, "MODEL_NAME", "gpt-5")
	assertEnvValueOnce(t, env, "OTEL_EXPORTER_OTLP_ENDPOINT", "http://wrong:4317")
}

func TestBuildBYOAgentRejectsHTTPProtocol(t *testing.T) {
	in := byoApplyInput()
	protocol := v1alpha1.AgentProtocolHTTP
	in.Target.(*v1alpha1.Agent).Spec.Source.Protocol = &protocol

	_, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{})
	assert.ErrorIs(t, err, errUnsupported)
}

func TestBuildBYOAgentRejectsHarness(t *testing.T) {
	in := byoApplyInput()
	in.Deployment.Spec.Harness = &v1alpha1.DeploymentHarness{Type: "claude-code"}
	_, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{})
	assert.ErrorIs(t, err, errUnsupported)
}

func assertEnvValueOnce(t *testing.T, env []corev1.EnvVar, name, value string) {
	t.Helper()
	var matches []corev1.EnvVar
	for _, item := range env {
		if item.Name == name {
			matches = append(matches, item)
		}
	}
	require.Len(t, matches, 1)
	assert.Equal(t, value, matches[0].Value)
}

func envValue(t *testing.T, env []corev1.EnvVar, name string) string {
	t.Helper()
	for _, item := range env {
		if item.Name == name {
			return item.Value
		}
	}
	t.Fatalf("environment variable %q not found", name)
	return ""
}

func TestBuildBYOAgentRejectsDeclarative(t *testing.T) {
	in := byoApplyInput()
	in.Target.(*v1alpha1.Agent).Spec.Source = nil
	_, err := buildTestBYOAgent(context.Background(), in, runtimeConfig{})
	require.ErrorIs(t, err, errUnsupported)
	assert.ErrorContains(t, err, "kagent agent requires spec.source.image")
}

func TestBuildToolServerRemoteStreamableHTTPWithHeader(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "gh-mcp", Namespace: "default"},
		Spec: v1alpha1.MCPServerSpec{
			Remote: &v1alpha1.MCPRemote{
				Type: "streamable-http", URL: "https://mcp.example.com/mcp",
				Headers: []v1alpha1.HTTPHeader{{Name: "X-K", Value: "v"}},
			},
		},
	}
	got, err := buildToolServer(in, runtimeConfig{Namespace: "kagent"}, deployConfig{})
	require.NoError(t, err)
	assert.Equal(t, "RemoteMCPServer", got.Kind)
	assert.Equal(t, WorkloadName(in.Target.GetMetadata().Name), got.Name())
	assert.Equal(t, "kagent", got.Namespace())
	require.NotNil(t, got.Remote)
	assert.Equal(t, remoteMCPProtocolStreamableHTTP, got.Remote.Spec.Protocol)
	assert.Equal(t, "https://mcp.example.com/mcp", got.Remote.Spec.URL)
	assert.Equal(t, WorkloadName(in.Target.GetMetadata().Name), got.Remote.Spec.Description)
}

func TestBuildToolServerRemoteUsesKagentStreamableHTTPProtocol(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "gh-mcp", Namespace: "default"},
		Spec: v1alpha1.MCPServerSpec{
			Remote: &v1alpha1.MCPRemote{Type: "sse", URL: "https://mcp.example.com/sse"},
		},
	}
	got, err := buildToolServer(in, runtimeConfig{Namespace: "kagent"}, deployConfig{})
	require.NoError(t, err)
	require.NotNil(t, got.Remote)
	assert.Equal(t, remoteMCPProtocolStreamableHTTP, got.Remote.Spec.Protocol)
	assert.Equal(t, "https://mcp.example.com/sse", got.Remote.Spec.URL)
}

func TestBuildToolServerRemoteRejectsSecretRefs(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "remote-mcp"},
		Spec: v1alpha1.MCPServerSpec{
			Remote: &v1alpha1.MCPRemote{URL: "https://mcp.example.com/mcp"},
		},
	}

	_, err := buildToolServer(
		in,
		runtimeConfig{Namespace: "kagent"},
		deployConfig{SecretRefs: []string{"mcp-token"}},
	)
	require.ErrorContains(t, err, "secretRefs is not supported for remote")
}

func TestBuildToolServerNPMPackage(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "fs-mcp", Namespace: "default"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type: v1alpha1.MCPPackageOriginTypeNPM, Identifier: "@acme/fs-mcp",
					NPM: &v1alpha1.MCPPackageOriginNPM{Version: "1.2.3", ServerName: "fs"},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			}},
		},
	}
	got, err := buildToolServer(in, runtimeConfig{
		ImagePullSecrets: []string{"registry-creds"},
		Deployment: runtimeDeploymentConfig{
			NodeSelector: map[string]string{"node-group": "mcp"},
			Tolerations: []corev1.Toleration{{
				Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "mcp", Effect: corev1.TaintEffectNoSchedule,
			}},
			Affinity: &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}},
		},
	}, deployConfig{})
	require.NoError(t, err)
	assert.Equal(t, "MCPServer", got.Kind)
	require.NotNil(t, got.MCP)
	assert.Equal(t, transportTypeStdio, got.MCP.Spec.TransportType)
	require.NotNil(t, got.MCP.Spec.StdioTransport)
	assert.Equal(t, types.DefaultNPMRunnerImage, got.MCP.Spec.Deployment.Image)
	assert.Equal(t, "npx", got.MCP.Spec.Deployment.Cmd)
	assert.Equal(t, []string{"-y", "@acme/fs-mcp@1.2.3"}, got.MCP.Spec.Deployment.Args)
	assert.Equal(t, []corev1.LocalObjectReference{{Name: "registry-creds"}}, got.MCP.Spec.Deployment.ImagePullSecrets)
	assert.Equal(t, map[string]string{"node-group": "mcp"}, got.MCP.Spec.Deployment.NodeSelector)
	assert.Equal(t, []corev1.Toleration{{
		Key: "dedicated", Operator: corev1.TolerationOpEqual, Value: "mcp", Effect: corev1.TaintEffectNoSchedule,
	}}, got.MCP.Spec.Deployment.Tolerations)
	assert.Equal(t, &corev1.Affinity{NodeAffinity: &corev1.NodeAffinity{}}, got.MCP.Spec.Deployment.Affinity)
}

func TestBuildToolServerPyPIPackage(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "py-mcp", Namespace: "default"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type: v1alpha1.MCPPackageOriginTypePyPI, Identifier: "acme-mcp",
					PyPI: &v1alpha1.MCPPackageOriginPyPI{Version: "0.4.0"},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			}},
		},
	}
	got, err := buildToolServer(in, runtimeConfig{}, deployConfig{})
	require.NoError(t, err)
	require.NotNil(t, got.MCP)
	assert.Equal(t, types.DefaultPyPIRunnerImage, got.MCP.Spec.Deployment.Image)
	assert.Equal(t, "uvx", got.MCP.Spec.Deployment.Cmd)
	assert.Equal(t, []string{"acme-mcp==0.4.0"}, got.MCP.Spec.Deployment.Args)
}

func TestBuildToolServerHTTPPackage(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "http-mcp"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type: v1alpha1.MCPPackageOriginTypeNPM, Identifier: "@acme/http-mcp",
					NPM: &v1alpha1.MCPPackageOriginNPM{Version: "1.0.0", ServerName: "http"},
				},
				Launch:    &v1alpha1.MCPPackageLaunch{Command: "serve"},
				Transport: v1alpha1.MCPTransport{Type: "http", Port: 8080, Path: "/mcp"},
			}},
		},
	}
	got, err := buildToolServer(in, runtimeConfig{}, deployConfig{})
	require.NoError(t, err)
	require.NotNil(t, got.MCP)
	assert.Equal(t, transportTypeHTTP, got.MCP.Spec.TransportType)
	assert.Equal(t, uint16(8080), got.MCP.Spec.Deployment.Port)
	assert.Equal(t, "8080", got.MCP.Spec.Deployment.Env["PORT"])
	require.NotNil(t, got.MCP.Spec.HTTPTransport)
	assert.Equal(t, uint32(8080), got.MCP.Spec.HTTPTransport.TargetPort)
	assert.Equal(t, "/mcp", got.MCP.Spec.HTTPTransport.TargetPath)
}

func TestBuildToolServerIncludesSecretRefs(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "fs-mcp"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type: v1alpha1.MCPPackageOriginTypeNPM, Identifier: "@acme/fs-mcp",
					NPM: &v1alpha1.MCPPackageOriginNPM{Version: "1.2.3", ServerName: "fs"},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			}},
		},
	}
	got, err := buildToolServer(in, runtimeConfig{}, deployConfig{SecretRefs: []string{"s1", "shared"}})
	require.NoError(t, err)
	require.NotNil(t, got.MCP)
	assert.Equal(t, []corev1.LocalObjectReference{
		{Name: "s1"},
		{Name: "shared"},
	}, got.MCP.Spec.Deployment.SecretRefs)
}

func TestBuildToolServerRejectsMissingRequiredEnvironment(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "required-env"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type: v1alpha1.MCPPackageOriginTypeNPM, Identifier: "required-env",
					NPM: &v1alpha1.MCPPackageOriginNPM{Version: "1.0.0"},
				},
				Launch: &v1alpha1.MCPPackageLaunch{
					Command: "server",
					Env:     []v1alpha1.MCPKeyValueInput{{Name: "API_KEY", IsRequired: true}},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			}},
		},
	}

	_, err := buildToolServer(in, runtimeConfig{}, deployConfig{})
	require.ErrorContains(t, err, "missing required environment variables: API_KEY")
}

func TestBuildToolServerOCI(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "oci-mcp"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type: v1alpha1.MCPPackageOriginTypeOCI, Identifier: "ghcr.io/x/y:1",
					OCI: &v1alpha1.MCPPackageOriginOCI{ServerName: "oci"},
				},
				Launch: &v1alpha1.MCPPackageLaunch{
					Command: "server",
					Args:    []v1alpha1.MCPArgument{{Type: v1alpha1.MCPArgumentTypePositional, Value: "--stdio"}},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			}},
		},
	}
	got, err := buildToolServer(in, runtimeConfig{}, deployConfig{})
	require.NoError(t, err)
	require.NotNil(t, got.MCP)
	assert.Equal(t, "ghcr.io/x/y:1", got.MCP.Spec.Deployment.Image)
	assert.Equal(t, "server", got.MCP.Spec.Deployment.Cmd)
	assert.Equal(t, []string{"--stdio"}, got.MCP.Spec.Deployment.Args)
}

func TestBuildToolServerHTTPOCIUsesImageEntrypoint(t *testing.T) {
	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "oci-http-mcp"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type: v1alpha1.MCPPackageOriginTypeOCI, Identifier: "ghcr.io/x/http-mcp:1",
					OCI: &v1alpha1.MCPPackageOriginOCI{ServerName: "oci-http"},
				},
				Transport: v1alpha1.MCPTransport{Type: "http", Port: 8080, Path: "/mcp"},
			}},
		},
	}

	got, err := buildToolServer(in, runtimeConfig{}, deployConfig{})
	require.NoError(t, err)
	require.NotNil(t, got.MCP)
	assert.Equal(t, "ghcr.io/x/http-mcp:1", got.MCP.Spec.Deployment.Image)
	assert.Empty(t, got.MCP.Spec.Deployment.Cmd)
	assert.Empty(t, got.MCP.Spec.Deployment.Args)
}

func TestPackageRunnerPreservesNamedArgument(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type: v1alpha1.MCPPackageOriginTypeOCI,
			OCI:  &v1alpha1.MCPPackageOriginOCI{},
		},
		Launch: &v1alpha1.MCPPackageLaunch{
			Command: "server",
			Args: []v1alpha1.MCPArgument{{
				Type:  v1alpha1.MCPArgumentTypeNamed,
				Name:  "--port",
				Value: "3000",
			}},
		},
	}

	_, _, args, err := packageRunner(pkg)
	require.NoError(t, err)
	assert.Equal(t, []string{"--port", "3000"}, args)
}

func TestPackageRunnerExplicitEmptyLaunchDoesNotRestoreDefaults(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type:       v1alpha1.MCPPackageOriginTypeNPM,
			Identifier: "example-server",
			NPM:        &v1alpha1.MCPPackageOriginNPM{Version: "1.0.0"},
		},
		Launch: &v1alpha1.MCPPackageLaunch{},
	}

	_, command, args, err := packageRunner(pkg)
	require.NoError(t, err)
	assert.Empty(t, command)
	assert.Empty(t, args)
}

func TestPackageRunnerExplicitArgsDoNotRestoreDefaultCommand(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type:       v1alpha1.MCPPackageOriginTypeNPM,
			Identifier: "example-server",
			NPM:        &v1alpha1.MCPPackageOriginNPM{Version: "1.0.0"},
		},
		Launch: &v1alpha1.MCPPackageLaunch{Args: []v1alpha1.MCPArgument{{
			Type:  v1alpha1.MCPArgumentTypeNamed,
			Name:  "--port",
			Value: "3000",
		}}},
	}

	_, command, args, err := packageRunner(pkg)
	require.NoError(t, err)
	assert.Empty(t, command)
	assert.Equal(t, []string{"--port", "3000"}, args)
}

func TestPackageRunnerPreservesOCIEntrypointArguments(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type:       v1alpha1.MCPPackageOriginTypeOCI,
			Identifier: "ghcr.io/acme/server:1",
			OCI:        &v1alpha1.MCPPackageOriginOCI{},
		},
		Launch: &v1alpha1.MCPPackageLaunch{
			Args: []v1alpha1.MCPArgument{{
				Type:  v1alpha1.MCPArgumentTypeNamed,
				Name:  "--port",
				Value: "3000",
			}},
		},
	}

	image, command, args, err := packageRunner(pkg)
	require.NoError(t, err)
	assert.Equal(t, "ghcr.io/acme/server:1", image)
	assert.Empty(t, command)
	assert.Equal(t, []string{"--port", "3000"}, args)
}

func TestPackageRunnerOmitsEmptyNPMVersionSuffix(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type:       v1alpha1.MCPPackageOriginTypeNPM,
			Identifier: "example-server",
			NPM:        &v1alpha1.MCPPackageOriginNPM{},
		},
	}

	_, command, args, err := packageRunner(pkg)
	require.NoError(t, err)
	assert.Equal(t, "npx", command)
	assert.Equal(t, []string{"-y", "example-server"}, args)
}

func TestPackageRunnerOmitsEmptyPyPIVersionSuffix(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type:       v1alpha1.MCPPackageOriginTypePyPI,
			Identifier: "example-server",
			PyPI:       &v1alpha1.MCPPackageOriginPyPI{},
		},
	}

	_, command, args, err := packageRunner(pkg)
	require.NoError(t, err)
	assert.Equal(t, "uvx", command)
	assert.Equal(t, []string{"example-server"}, args)
}

func TestPackageRunnerPlacesPositionalArgumentsBeforeNamedArguments(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type:       v1alpha1.MCPPackageOriginTypeOCI,
			Identifier: "example-server",
			OCI:        &v1alpha1.MCPPackageOriginOCI{},
		},
		Launch: &v1alpha1.MCPPackageLaunch{
			Command: "server",
			Args: []v1alpha1.MCPArgument{
				{Type: v1alpha1.MCPArgumentTypeNamed, Name: "--port", Value: "3000"},
				{Type: v1alpha1.MCPArgumentTypePositional, Value: "serve"},
			},
		},
	}

	_, _, args, err := packageRunner(pkg)
	require.NoError(t, err)
	assert.Equal(t, []string{"serve", "--port", "3000"}, args)
}

func TestPackageRunnerRejectsMultipleOriginConfigurations(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{
		Origin: v1alpha1.MCPPackageOrigin{
			Type:       v1alpha1.MCPPackageOriginTypeNPM,
			Identifier: "example-server",
			NPM:        &v1alpha1.MCPPackageOriginNPM{},
			OCI:        &v1alpha1.MCPPackageOriginOCI{},
		},
	}

	image, command, args, err := packageRunner(pkg)
	require.Empty(t, image)
	require.Empty(t, command)
	require.Nil(t, args)
	require.ErrorContains(t, err, "exactly one")
}

func TestPackageEnvOmitsEmptyOverrideForDeclaredVariable(t *testing.T) {
	pkg := &v1alpha1.MCPPackage{Launch: &v1alpha1.MCPPackageLaunch{
		Env: []v1alpha1.MCPKeyValueInput{{Name: "OPTIONAL", Value: "default"}},
	}}
	in := byoApplyInput()
	in.Deployment.Spec.Env = map[string]string{
		"OPTIONAL":   "",
		"UNDECLARED": "",
	}

	env, err := packageEnv(pkg, in)
	require.NoError(t, err)
	assert.NotContains(t, env, "OPTIONAL")
	assert.Contains(t, env, "UNDECLARED")
}
