package utils

import (
	"context"
	"encoding/json"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestSpecToPlatformRemoteMCPServer_RemoteTransport(t *testing.T) {
	spec := v1alpha1.RemoteMCPServerSpec{
		Description: "weather",
		Remote: v1alpha1.MCPTransport{
			Type: "streamable-http",
			URL:  "https://api.weather.example/mcp",
			Headers: []v1alpha1.MCPKeyValueInput{{
				Name:  "X-Token",
				Value: "supersecret",
			}},
		},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "default", Name: "weather", Version: "1.0.0"}

	got, err := SpecToPlatformRemoteMCPServer(context.Background(), meta, spec, RemoteMCPServerTranslateOpts{
		DeploymentID: "dep-1",
	})
	if err != nil {
		t.Fatalf("SpecToPlatformRemoteMCPServer: %v", err)
	}
	if got.MCPServerType != platformtypes.MCPServerTypeRemote {
		t.Fatalf("MCPServerType = %q, want %q", got.MCPServerType, platformtypes.MCPServerTypeRemote)
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

func TestSpecToPlatformMCPServer_OCIPackage(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Packages: []v1alpha1.MCPPackage{{
			RegistryType: "oci",
			Identifier:   "ghcr.io/example/mcp:v0.1.0",
			Transport:    v1alpha1.MCPTransport{Type: "stdio"},
		}},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "default", Name: "example", Version: "0.1.0"}

	got, err := SpecToPlatformMCPServer(context.Background(), meta, spec, MCPServerTranslateOpts{DeploymentID: "dep-2"})
	if err != nil {
		t.Fatalf("SpecToPlatformMCPServer: %v", err)
	}
	if got.MCPServerType != platformtypes.MCPServerTypeLocal {
		t.Fatalf("MCPServerType = %q", got.MCPServerType)
	}
	if got.Local.Deployment.Image != "ghcr.io/example/mcp:v0.1.0" {
		t.Fatalf("Image = %q", got.Local.Deployment.Image)
	}
}

func TestSpecToPlatformMCPServer_NamespaceOptOverridesMeta(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Packages: []v1alpha1.MCPPackage{{
			RegistryType: "oci",
			Identifier:   "ghcr.io/example/mcp:v1",
			Transport:    v1alpha1.MCPTransport{Type: "stdio"},
		}},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "team-a", Name: "example", Version: "1.0.0"}

	got, err := SpecToPlatformMCPServer(context.Background(), meta, spec, MCPServerTranslateOpts{
		DeploymentID: "dep-3",
		Namespace:    "platform",
	})
	if err != nil {
		t.Fatalf("SpecToPlatformMCPServer: %v", err)
	}
	if got.Namespace != "platform" {
		t.Fatalf("Namespace = %q, want platform (opts override)", got.Namespace)
	}
}

func TestSpecToPlatformAgent_ResolvesMCPServerRefs(t *testing.T) {
	mcp := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "tools", Version: "1.0.0"},
		Spec: v1alpha1.MCPServerSpec{
			Packages: []v1alpha1.MCPPackage{{
				RegistryType: "oci",
				Identifier:   "ghcr.io/example/tools:v1",
				Transport:    v1alpha1.MCPTransport{Type: "stdio"},
			}},
		},
	}
	var getterCalls []v1alpha1.ResourceRef
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		getterCalls = append(getterCalls, ref)
		return mcp, nil
	}

	agentMeta := v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Version: "1.0.0"}
	agentSpec := v1alpha1.AgentSpec{
		Source:        &v1alpha1.AgentSource{Image: "ghcr.io/example/alice:v1"},
		ModelProvider: "openai",
		ModelName:     "gpt-4o",
		MCPServers: []v1alpha1.ResourceRef{
			{Kind: v1alpha1.KindMCPServer, Name: "tools", Version: "1.0.0"},
		},
	}

	agent, servers, err := SpecToPlatformAgent(context.Background(), agentMeta, agentSpec, AgentTranslateOpts{
		DeploymentID:  "dep-42",
		KagentURL:     "http://localhost",
		DeploymentEnv: map[string]string{"EXTRA": "value"},
		Getter:        getter,
	})
	if err != nil {
		t.Fatalf("SpecToPlatformAgent: %v", err)
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
	encoded := agent.Deployment.Env["MCP_SERVERS_CONFIG"]
	if encoded == "" {
		t.Fatalf("MCP_SERVERS_CONFIG missing")
	}
	var decoded []platformtypes.ResolvedMCPServerConfig
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

func TestSpecToPlatformAgent_ResolvesRemoteMCPServerHeaders(t *testing.T) {
	remote := &v1alpha1.RemoteMCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindRemoteMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "remote-tools", Version: "1.0.0"},
		Spec: v1alpha1.RemoteMCPServerSpec{
			Remote: v1alpha1.MCPTransport{
				Type: "streamable-http",
				URL:  "https://remote.example/mcp",
				Headers: []v1alpha1.MCPKeyValueInput{
					{Name: "Authorization", IsRequired: true},
					{Name: "X-Trace", Default: "trace-default"},
				},
			},
		},
	}
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return remote, nil
	}

	agent, servers, err := SpecToPlatformAgent(
		context.Background(),
		v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Version: "1.0.0"},
		v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{
				{Kind: v1alpha1.KindRemoteMCPServer, Name: "remote-tools", Version: "1.0.0"},
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
		t.Fatalf("SpecToPlatformAgent: %v", err)
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

	var decoded []platformtypes.ResolvedMCPServerConfig
	if err := json.Unmarshal([]byte(agent.Deployment.Env["MCP_SERVERS_CONFIG"]), &decoded); err != nil {
		t.Fatalf("decode MCP_SERVERS_CONFIG: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Headers["Authorization"] != "Bearer token" || decoded[0].Headers["X-Trace"] != "trace-default" {
		t.Fatalf("decoded MCP_SERVERS_CONFIG = %+v", decoded)
	}
}

func TestSpecToPlatformAgent_NamespaceOptWinsOverMeta(t *testing.T) {
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		t.Fatalf("getter should not be called when no refs; got %+v", ref)
		return nil, nil
	}
	agentMeta := v1alpha1.ObjectMeta{Namespace: "team-a", Name: "alice", Version: "1.0.0"}
	agent, _, err := SpecToPlatformAgent(context.Background(), agentMeta, v1alpha1.AgentSpec{}, AgentTranslateOpts{
		DeploymentID: "dep-ns",
		Namespace:    "kagent",
		Getter:       getter,
	})
	if err != nil {
		t.Fatalf("SpecToPlatformAgent: %v", err)
	}
	if agent.Deployment.Env["KAGENT_NAMESPACE"] != "kagent" {
		t.Fatalf("KAGENT_NAMESPACE = %q, want kagent", agent.Deployment.Env["KAGENT_NAMESPACE"])
	}
}

func TestSpecToPlatformAgent_DanglingRefPropagates(t *testing.T) {
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return nil, v1alpha1.ErrDanglingRef
	}
	agentMeta := v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Version: "1.0.0"}
	agentSpec := v1alpha1.AgentSpec{
		MCPServers: []v1alpha1.ResourceRef{
			{Kind: v1alpha1.KindMCPServer, Name: "missing", Version: "1.0.0"},
		},
	}
	_, _, err := SpecToPlatformAgent(context.Background(), agentMeta, agentSpec, AgentTranslateOpts{Getter: getter})
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
