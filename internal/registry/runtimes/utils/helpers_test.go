package utils

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	runtimetypes "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestSpecToRuntimeMCPServer_RemoteTransport(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Description: "weather",
		Remote: &v1alpha1.MCPRemote{
			Type: "streamable-http",
			URL:  "https://api.weather.example/mcp",
			Headers: []v1alpha1.HTTPHeader{{
				Name:  "X-Token",
				Value: "supersecret",
			}},
		},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "default", Name: "weather", Tag: "1.0.0"}

	got, err := SpecToRuntimeMCPServer(context.Background(), meta, spec, MCPServerTranslateOpts{
		DeploymentID: "dep-1",
	})
	if err != nil {
		t.Fatalf("SpecToRuntimeMCPServer: %v", err)
	}
	if got.MCPServerType != runtimetypes.MCPServerTypeRemote {
		t.Fatalf("MCPServerType = %q, want %q", got.MCPServerType, runtimetypes.MCPServerTypeRemote)
	}
	if got.Remote == nil {
		t.Fatalf("Remote is nil")
	}
	if got.Remote.Host != "api.weather.example" {
		t.Fatalf("Remote.Host = %q, want api.weather.example", got.Remote.Host)
	}
	if got.Remote.Scheme != "https" || got.Remote.Port != 443 {
		t.Fatalf("Remote scheme/port = %q/%d", got.Remote.Scheme, got.Remote.Port)
	}
	if got.Namespace != "default" {
		t.Fatalf("Namespace = %q, want default (from meta)", got.Namespace)
	}
	if got.DeploymentID != "dep-1" {
		t.Fatalf("DeploymentID = %q", got.DeploymentID)
	}
}

func TestSpecToRuntimeMCPServer_OCIPackage(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Source: &v1alpha1.MCPServerSource{
			Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type:       v1alpha1.MCPPackageOriginTypeOCI,
					Identifier: "ghcr.io/example/mcp:v0.1.0",
					OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "example"},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			},
		},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "default", Name: "example", Tag: "0.1.0"}

	got, err := SpecToRuntimeMCPServer(context.Background(), meta, spec, MCPServerTranslateOpts{DeploymentID: "dep-2"})
	if err != nil {
		t.Fatalf("SpecToRuntimeMCPServer: %v", err)
	}
	if got.MCPServerType != runtimetypes.MCPServerTypeLocal {
		t.Fatalf("MCPServerType = %q", got.MCPServerType)
	}
	if got.Local.Deployment.Image != "ghcr.io/example/mcp:v0.1.0" {
		t.Fatalf("Image = %q", got.Local.Deployment.Image)
	}
}

func TestSpecToRuntimeMCPServer_NamespaceOptOverridesMeta(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Source: &v1alpha1.MCPServerSource{
			Package: &v1alpha1.MCPPackage{
				Origin: v1alpha1.MCPPackageOrigin{
					Type:       v1alpha1.MCPPackageOriginTypeOCI,
					Identifier: "ghcr.io/example/mcp:v1",
					OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "example"},
				},
				Transport: v1alpha1.MCPTransport{Type: "stdio"},
			},
		},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "team-a", Name: "example", Tag: "1.0.0"}

	got, err := SpecToRuntimeMCPServer(context.Background(), meta, spec, MCPServerTranslateOpts{
		DeploymentID: "dep-3",
		Namespace:    "runtime",
	})
	if err != nil {
		t.Fatalf("SpecToRuntimeMCPServer: %v", err)
	}
	if got.Namespace != "runtime" {
		t.Fatalf("Namespace = %q, want runtime (opts override)", got.Namespace)
	}
}

func TestResolveDeploymentModelSpec_NormalizesAndResolves(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		tag       string
		wantNS    string
		wantTag   string
	}{
		{
			name:    "deployment namespace and latest tag",
			wantNS:  "team-a",
			wantTag: "latest",
		},
		{
			name:      "explicit namespace and tag",
			namespace: "models",
			tag:       "approved-v1",
			wantNS:    "models",
			wantTag:   "approved-v1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := &v1alpha1.Deployment{
				Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "assistant"},
				Spec: v1alpha1.DeploymentSpec{
					ModelRef: &v1alpha1.ModelRef{
						Namespace: tt.namespace,
						Name:      "approved-model",
						Tag:       tt.tag,
					},
				},
			}
			var gotRef v1alpha1.ResourceRef
			got, err := ResolveDeploymentModelSpec(t.Context(), deployment, func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
				gotRef = ref
				return &v1alpha1.Model{
					TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindModel},
					Spec: v1alpha1.ModelSpec{
						Provider: v1alpha1.ModelProviderBedrock,
						Model:    "us.anthropic.claude-sonnet-4-6",
					},
				}, nil
			})
			if err != nil {
				t.Fatalf("ResolveDeploymentModelSpec: %v", err)
			}
			if gotRef.Kind != v1alpha1.KindModel || gotRef.Namespace != tt.wantNS ||
				gotRef.Name != "approved-model" || gotRef.Tag != tt.wantTag {
				t.Fatalf("normalized ref = %+v", gotRef)
			}
			if got.Provider != v1alpha1.ModelProviderBedrock || got.Model != "us.anthropic.claude-sonnet-4-6" {
				t.Fatalf("resolved model = %+v", got)
			}
		})
	}
}

func TestResolveDeploymentModelSpec_UsesDefaultHarnessModel(t *testing.T) {
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "assistant"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: "assistant"},
			Harness:   &v1alpha1.DeploymentHarness{Type: "claude-code"},
		},
	}
	var gotRef v1alpha1.ResourceRef
	got, err := ResolveDeploymentModelSpec(t.Context(), deployment, func(_ context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		gotRef = ref
		return &v1alpha1.Model{
			TypeMeta: v1alpha1.TypeMeta{Kind: v1alpha1.KindModel},
			Spec: v1alpha1.ModelSpec{
				Provider: v1alpha1.ModelProviderBedrock,
				Model:    "us.anthropic.claude-sonnet-4-6",
			},
		}, nil
	})
	if err != nil {
		t.Fatalf("ResolveDeploymentModelSpec: %v", err)
	}
	if gotRef.Kind != v1alpha1.KindModel || gotRef.Namespace != "team-a" ||
		gotRef.Name != v1alpha1.DefaultModelName || gotRef.Tag != "latest" {
		t.Fatalf("default model ref = %+v", gotRef)
	}
	if got.Provider != v1alpha1.ModelProviderBedrock {
		t.Fatalf("resolved model = %+v", got)
	}
}

func TestResolveDeploymentModelSpec_FailuresNameNormalizedRef(t *testing.T) {
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "assistant"},
		Spec: v1alpha1.DeploymentSpec{
			ModelRef: &v1alpha1.ModelRef{Name: "approved-model"},
		},
	}

	_, err := ResolveDeploymentModelSpec(t.Context(), deployment, nil)
	if err == nil || !strings.Contains(err.Error(), "spec.modelRef") ||
		!strings.Contains(err.Error(), "Model team-a/approved-model@latest") {
		t.Fatalf("missing getter error = %v", err)
	}

	_, err = ResolveDeploymentModelSpec(t.Context(), deployment, func(context.Context, v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return nil, v1alpha1.ErrDanglingRef
	})
	if !errors.Is(err, v1alpha1.ErrDanglingRef) || !strings.Contains(err.Error(), "spec.modelRef") {
		t.Fatalf("dangling ref error = %v", err)
	}

	_, err = ResolveDeploymentModelSpec(t.Context(), deployment, func(context.Context, v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return &v1alpha1.Agent{}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "unexpected type") ||
		!strings.Contains(err.Error(), "Model team-a/approved-model@latest") {
		t.Fatalf("unexpected type error = %v", err)
	}
}

func TestSpecToRuntimeAgent_ModelEnvIsAuthoritative(t *testing.T) {
	agent, _, err := SpecToRuntimeAgent(
		t.Context(),
		v1alpha1.ObjectMeta{Namespace: "default", Name: "alice"},
		v1alpha1.AgentSpec{},
		AgentTranslateOpts{
			DeploymentEnv: map[string]string{
				"MODEL_PROVIDER": "deployment-provider",
				"MODEL_NAME":     "deployment-model",
			},
			Model: &v1alpha1.ModelSpec{
				Provider: v1alpha1.ModelProviderBedrock,
				Model:    "us.anthropic.claude-opus-4-8",
			},
		},
	)
	if err != nil {
		t.Fatalf("SpecToRuntimeAgent: %v", err)
	}
	if agent.Deployment.Env["MODEL_PROVIDER"] != v1alpha1.ModelProviderBedrock ||
		agent.Deployment.Env["MODEL_NAME"] != "us.anthropic.claude-opus-4-8" {
		t.Fatalf("model env = %+v", agent.Deployment.Env)
	}

	agent, _, err = SpecToRuntimeAgent(
		t.Context(),
		v1alpha1.ObjectMeta{Namespace: "default", Name: "alice"},
		v1alpha1.AgentSpec{},
		AgentTranslateOpts{DeploymentEnv: map[string]string{
			"MODEL_PROVIDER": "deployment-provider",
			"MODEL_NAME":     "deployment-model",
		}},
	)
	if err != nil {
		t.Fatalf("SpecToRuntimeAgent without Model: %v", err)
	}
	if _, ok := agent.Deployment.Env["MODEL_PROVIDER"]; ok {
		t.Fatalf("MODEL_PROVIDER should be omitted without Model: %+v", agent.Deployment.Env)
	}
	if _, ok := agent.Deployment.Env["MODEL_NAME"]; ok {
		t.Fatalf("MODEL_NAME should be omitted without Model: %+v", agent.Deployment.Env)
	}
}

func TestSpecToRuntimeAgent_ResolvesMCPServerRefs(t *testing.T) {
	mcp := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "tools", Tag: "1.0.0"},
		Spec: v1alpha1.MCPServerSpec{
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					Origin: v1alpha1.MCPPackageOrigin{
						Type:       v1alpha1.MCPPackageOriginTypeOCI,
						Identifier: "ghcr.io/example/tools:v1",
						OCI:        &v1alpha1.MCPPackageOriginOCI{ServerName: "tools"},
					},
					Transport: v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	}
	var getterCalls []v1alpha1.ResourceRef
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		getterCalls = append(getterCalls, ref)
		return mcp, nil
	}

	agentMeta := v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Tag: "1.0.0"}
	agentSpec := v1alpha1.AgentSpec{
		Source: &v1alpha1.AgentSource{Image: "ghcr.io/example/alice:v1"},
		MCPServers: []v1alpha1.ResourceRef{
			{Kind: v1alpha1.KindMCPServer, Name: "tools", Tag: "1.0.0"},
		},
	}

	agent, servers, err := SpecToRuntimeAgent(context.Background(), agentMeta, agentSpec, AgentTranslateOpts{
		DeploymentID:  "dep-42",
		KagentURL:     "http://localhost",
		DeploymentEnv: map[string]string{"EXTRA": "value"},
		Model: &v1alpha1.ModelSpec{
			Provider: v1alpha1.ModelProviderBedrock,
			Model:    "us.anthropic.claude-sonnet-4-6",
		},
		Getter: getter,
	})
	if err != nil {
		t.Fatalf("SpecToRuntimeAgent: %v", err)
	}
	if len(getterCalls) != 1 {
		t.Fatalf("getter calls = %d, want 1", len(getterCalls))
	}
	if getterCalls[0].Namespace != "default" || getterCalls[0].Name != "tools" || getterCalls[0].Kind != v1alpha1.KindMCPServer {
		t.Fatalf("unexpected getter ref: %+v", getterCalls[0])
	}
	if agent.Deployment.Env["AGENT_NAME"] != "alice" {
		t.Fatalf("AGENT_NAME missing: %+v", agent.Deployment.Env)
	}
	if agent.Deployment.Env["KAGENT_URL"] != "http://localhost" {
		t.Fatalf("KAGENT_URL = %q, want http://localhost", agent.Deployment.Env["KAGENT_URL"])
	}
	if agent.Deployment.Env["EXTRA"] != "value" {
		t.Fatalf("EXTRA env missing: %+v", agent.Deployment.Env)
	}
	if agent.Deployment.Env["MODEL_PROVIDER"] != v1alpha1.ModelProviderBedrock ||
		agent.Deployment.Env["MODEL_NAME"] != "us.anthropic.claude-sonnet-4-6" {
		t.Fatalf("model env = %+v", agent.Deployment.Env)
	}
	encoded := agent.Deployment.Env["MCP_SERVERS_CONFIG"]
	if encoded == "" {
		t.Fatalf("MCP_SERVERS_CONFIG missing")
	}
	var decoded []runtimetypes.ResolvedMCPServerConfig
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("decode MCP_SERVERS_CONFIG: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Type != "command" {
		t.Fatalf("decoded MCP_SERVERS_CONFIG = %+v", decoded)
	}
	if len(servers) != 1 || servers[0].Local == nil || servers[0].Local.Deployment.Image != "ghcr.io/example/tools:v1" {
		t.Fatalf("resolved servers unexpected: %+v", servers)
	}
}

func TestSpecToRuntimeAgent_ResolvesRemoteMCPServerHeaders(t *testing.T) {
	remote := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "remote-tools", Tag: "1.0.0"},
		Spec: v1alpha1.MCPServerSpec{
			Remote: &v1alpha1.MCPRemote{
				Type: "streamable-http",
				URL:  "https://remote.example/mcp",
				Headers: []v1alpha1.HTTPHeader{
					{Name: "Authorization"},
					{Name: "X-Trace", Value: "trace-default"},
				},
			},
		},
	}
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return remote, nil
	}

	agent, servers, err := SpecToRuntimeAgent(
		context.Background(),
		v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Tag: "1.0.0"},
		v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{
				{Kind: v1alpha1.KindMCPServer, Name: "remote-tools", Tag: "1.0.0"},
			},
		},
		AgentTranslateOpts{
			DeploymentID: "dep-remote",
			HeaderValues: map[string]string{
				"Authorization": "Bearer token",
			},
			Getter: getter,
		},
	)
	if err != nil {
		t.Fatalf("SpecToRuntimeAgent: %v", err)
	}
	if len(servers) != 1 || servers[0].Remote == nil {
		t.Fatalf("resolved remote servers unexpected: %+v", servers)
	}
	headers := map[string]string{}
	for _, h := range servers[0].Remote.Headers {
		headers[h.Name] = h.Value
	}
	if headers["Authorization"] != "Bearer token" || headers["X-Trace"] != "trace-default" {
		t.Fatalf("translated headers = %+v", headers)
	}

	var decoded []runtimetypes.ResolvedMCPServerConfig
	if err := json.Unmarshal([]byte(agent.Deployment.Env["MCP_SERVERS_CONFIG"]), &decoded); err != nil {
		t.Fatalf("decode MCP_SERVERS_CONFIG: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Headers["Authorization"] != "Bearer token" || decoded[0].Headers["X-Trace"] != "trace-default" {
		t.Fatalf("decoded MCP_SERVERS_CONFIG = %+v", decoded)
	}
}

func TestSpecToRuntimeAgent_NamespaceOptWinsOverMeta(t *testing.T) {
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		t.Fatalf("getter should not be called when no refs; got %+v", ref)
		return nil, nil
	}
	agentMeta := v1alpha1.ObjectMeta{Namespace: "team-a", Name: "alice", Tag: "1.0.0"}
	agent, _, err := SpecToRuntimeAgent(context.Background(), agentMeta, v1alpha1.AgentSpec{}, AgentTranslateOpts{
		DeploymentID: "dep-ns",
		Namespace:    "kagent",
		Getter:       getter,
	})
	if err != nil {
		t.Fatalf("SpecToRuntimeAgent: %v", err)
	}
	if agent.Deployment.Env["KAGENT_NAMESPACE"] != "kagent" {
		t.Fatalf("KAGENT_NAMESPACE = %q, want kagent", agent.Deployment.Env["KAGENT_NAMESPACE"])
	}
}

func TestSpecToRuntimeAgent_DanglingRefPropagates(t *testing.T) {
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return nil, v1alpha1.ErrDanglingRef
	}
	agentMeta := v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Tag: "1.0.0"}
	agentSpec := v1alpha1.AgentSpec{
		MCPServers: []v1alpha1.ResourceRef{
			{Kind: v1alpha1.KindMCPServer, Name: "missing", Tag: "1.0.0"},
		},
	}
	_, _, err := SpecToRuntimeAgent(context.Background(), agentMeta, agentSpec, AgentTranslateOpts{Getter: getter})
	if err == nil {
		t.Fatalf("expected error for dangling ref")
	}
}

func TestSplitDeploymentRuntimeInputs_V1Alpha1Helper(t *testing.T) {
	in := map[string]string{
		"ENV_A":    "a",
		"ARG_foo":  "bar",
		"HEADER_X": "y",
		"PLAIN":    "v",
	}
	env, args, headers := SplitDeploymentRuntimeInputs(in)
	if env["ENV_A"] != "a" || env["PLAIN"] != "v" {
		t.Fatalf("env = %+v", env)
	}
	if args["foo"] != "bar" {
		t.Fatalf("args = %+v", args)
	}
	if headers["X"] != "y" {
		t.Fatalf("headers = %+v", headers)
	}
}
