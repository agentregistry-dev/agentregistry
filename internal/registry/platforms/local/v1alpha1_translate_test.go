package local

import (
	"context"
	"encoding/json"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestSpecToPlatformMCPServer_RemoteTransport(t *testing.T) {
	spec := v1alpha1.MCPServerSpec{
		Description: "weather",
		Remotes: []v1alpha1.MCPTransport{{
			Type: "streamable-http",
			URL:  "https://api.weather.example/mcp",
			Headers: []v1alpha1.MCPKeyValueInput{{
				Name:  "X-Token",
				Value: "supersecret",
			}},
		}},
	}
	meta := v1alpha1.ObjectMeta{Namespace: "default", Name: "weather", Version: "1.0.0"}

	got, err := specToPlatformMCPServer(context.Background(), meta, spec, "dep-1", true, nil, nil, nil)
	if err != nil {
		t.Fatalf("specToPlatformMCPServer: %v", err)
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
		t.Fatalf("Remote scheme/port = %q/%d, want https/443", got.Remote.Scheme, got.Remote.Port)
	}
	if got.Namespace != "default" {
		t.Fatalf("Namespace = %q, want default", got.Namespace)
	}
	if got.DeploymentID != "dep-1" {
		t.Fatalf("DeploymentID = %q, want dep-1", got.DeploymentID)
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

	got, err := specToPlatformMCPServer(context.Background(), meta, spec, "dep-2", false, nil, nil, nil)
	if err != nil {
		t.Fatalf("specToPlatformMCPServer: %v", err)
	}
	if got.MCPServerType != platformtypes.MCPServerTypeLocal {
		t.Fatalf("MCPServerType = %q, want %q", got.MCPServerType, platformtypes.MCPServerTypeLocal)
	}
	if got.Local == nil {
		t.Fatalf("Local is nil")
	}
	if got.Local.Deployment.Image != "ghcr.io/example/mcp:v0.1.0" {
		t.Fatalf("Image = %q", got.Local.Deployment.Image)
	}
	if got.Local.TransportType != platformtypes.TransportTypeStdio {
		t.Fatalf("TransportType = %q", got.Local.TransportType)
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
	)
	if err != nil {
		t.Fatalf("specToPlatformAgent: %v", err)
	}
	if len(getterCalls) != 1 {
		t.Fatalf("getter calls = %d, want 1", len(getterCalls))
	}
	if getterCalls[0].Namespace != "default" || getterCalls[0].Name != "tools" {
		t.Fatalf("getter ref = %+v", getterCalls[0])
	}
	if getterCalls[0].Kind != v1alpha1.KindMCPServer {
		t.Fatalf("getter ref.Kind = %q, want %q", getterCalls[0].Kind, v1alpha1.KindMCPServer)
	}
	if agent.Name != "alice" || agent.Version != "1.0.0" {
		t.Fatalf("agent identity = %s/%s", agent.Name, agent.Version)
	}
	if agent.Deployment.Image != "ghcr.io/example/alice:v1" {
		t.Fatalf("agent.Deployment.Image = %q", agent.Deployment.Image)
	}
	if agent.Deployment.Port != utils.DefaultLocalAgentPort {
		t.Fatalf("agent.Deployment.Port = %d, want %d", agent.Deployment.Port, utils.DefaultLocalAgentPort)
	}
	if agent.Deployment.Env["AGENT_NAME"] != "alice" {
		t.Fatalf("AGENT_NAME env missing; got %+v", agent.Deployment.Env)
	}
	if agent.Deployment.Env["KAGENT_NAMESPACE"] != "default" {
		t.Fatalf("KAGENT_NAMESPACE env missing; got %+v", agent.Deployment.Env)
	}
	if agent.Deployment.Env["EXTRA"] != "value" {
		t.Fatalf("EXTRA env missing; got %+v", agent.Deployment.Env)
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
	if len(servers) != 1 {
		t.Fatalf("resolved servers = %d, want 1", len(servers))
	}
	if servers[0].Local == nil || servers[0].Local.Deployment.Image != "ghcr.io/example/tools:v1" {
		t.Fatalf("resolved MCPServer unexpected: %+v", servers[0])
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
	_, _, err := specToPlatformAgent(context.Background(), agentMeta, agentSpec, "dep-x", nil, getter)
	if err == nil {
		t.Fatalf("expected error for dangling ref, got nil")
	}
}

func TestSplitDeploymentRuntimeInputs(t *testing.T) {
	in := map[string]string{
		"ENV_A":    "a",
		"ARG_foo":  "bar",
		"HEADER_X": "y",
		"PLAIN":    "v",
	}
	env, args, headers := splitDeploymentRuntimeInputs(in)
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
