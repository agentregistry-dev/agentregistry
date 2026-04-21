package kubernetes

import (
	"context"
	"encoding/json"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestK8sSpecToPlatformMCPServer_RemoteTransport(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Description: "weather",
		Remotes: []v1alpha1.MCPTransport{{
			Type: "streamable-http",
			URL:  "https://api.weather.example/mcp",
		}},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "default", Name: "weather", Version: "1.0.0"}

	got, err := specToPlatformMCPServer(context.Background(), meta, spec, "dep-1", true, nil, nil, nil, "kagent")
	if err != nil {
		t.Fatalf("specToPlatformMCPServer: %v", err)
	}
	if got.MCPServerType != platformtypes.MCPServerTypeRemote {
		t.Fatalf("MCPServerType = %q", got.MCPServerType)
	}
	if got.Namespace != "kagent" {
		t.Fatalf("Namespace = %q, want kagent (from namespace arg)", got.Namespace)
	}
}

func TestK8sSpecToPlatformMCPServer_NamespaceFallsBackToMeta(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Packages: []v1alpha1.MCPPackage{{
			RegistryType: "oci",
			Identifier:   "ghcr.io/example/mcp:v0.1.0",
			Transport:    v1alpha1.MCPTransport{Type: "stdio"},
		}},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "team-x", Name: "x", Version: "0.1.0"}

	got, err := specToPlatformMCPServer(context.Background(), meta, spec, "dep-2", false, nil, nil, nil, "")
	if err != nil {
		t.Fatalf("specToPlatformMCPServer: %v", err)
	}
	if got.Namespace != "team-x" {
		t.Fatalf("Namespace = %q, want team-x (from meta fallback)", got.Namespace)
	}
}

func TestK8sSpecToPlatformAgent_ResolvesMCPServerRefs(t *testing.T) {
	mcp := &v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "kagent", Name: "tools", Version: "1.0.0"},
		Spec: v1alpha1.MCPServerSpec{
			Remotes: []v1alpha1.MCPTransport{{Type: "streamable-http", URL: "https://remote.example/mcp"}},
		},
	}
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return mcp, nil
	}

	agentMeta := v1alpha1.ObjectMeta{Namespace: "kagent", Name: "alice", Version: "1.0.0"}
	agentSpec := v1alpha1.AgentSpec{
		Image:         "ghcr.io/example/alice:v1",
		ModelProvider: "openai",
		ModelName:     "gpt-4o",
		MCPServers: []v1alpha1.ResourceRef{
			{Kind: v1alpha1.KindMCPServer, Name: "tools", Version: "1.0.0"},
		},
	}

	agent, servers, err := specToPlatformAgent(
		context.Background(),
		agentMeta,
		agentSpec,
		"dep-42",
		map[string]string{"EXTRA": "value"},
		getter,
		"kagent",
	)
	if err != nil {
		t.Fatalf("specToPlatformAgent: %v", err)
	}
	if agent.Deployment.Env["KAGENT_NAMESPACE"] != "kagent" {
		t.Fatalf("KAGENT_NAMESPACE env = %q, want kagent", agent.Deployment.Env["KAGENT_NAMESPACE"])
	}
	if agent.Deployment.Env["KAGENT_URL"] == "" {
		t.Fatalf("KAGENT_URL env missing")
	}
	encoded := agent.Deployment.Env["MCP_SERVERS_CONFIG"]
	if encoded == "" {
		t.Fatalf("MCP_SERVERS_CONFIG missing")
	}
	var decoded []platformtypes.ResolvedMCPServerConfig
	if err := json.Unmarshal([]byte(encoded), &decoded); err != nil {
		t.Fatalf("decode MCP_SERVERS_CONFIG: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Type != "remote" {
		t.Fatalf("decoded MCP_SERVERS_CONFIG = %+v", decoded)
	}
	if len(servers) != 1 {
		t.Fatalf("resolved servers = %d, want 1", len(servers))
	}
	if servers[0].Namespace != "kagent" {
		t.Fatalf("resolved server namespace = %q, want kagent", servers[0].Namespace)
	}
}

func TestK8sSpecToPlatformAgent_DanglingRefPropagates(t *testing.T) {
	getter := func(ctx context.Context, ref v1alpha1.ResourceRef) (v1alpha1.Object, error) {
		return nil, v1alpha1.ErrDanglingRef
	}
	agentMeta := v1alpha1.ObjectMeta{Namespace: "kagent", Name: "alice", Version: "1.0.0"}
	agentSpec := v1alpha1.AgentSpec{
		MCPServers: []v1alpha1.ResourceRef{
			{Kind: v1alpha1.KindMCPServer, Name: "missing", Version: "1.0.0"},
		},
	}
	_, _, err := specToPlatformAgent(context.Background(), agentMeta, agentSpec, "dep-x", nil, getter, "kagent")
	if err == nil {
		t.Fatalf("expected error for dangling ref")
	}
}
