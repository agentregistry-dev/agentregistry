package utils

import (
	"context"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

type fakePlatformRuntimeRegistry struct {
	agentResp         *models.AgentResponse
	getAgentFn        func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	resolveSkillsFn   func(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error)
	resolvePromptsFn  func(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error)
	getServerByVerFn  func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	getProviderByIDFn func(ctx context.Context, providerID string) (*models.Provider, error)
}

func (f *fakePlatformRuntimeRegistry) ListServers(context.Context, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	return nil, "", nil
}

func (f *fakePlatformRuntimeRegistry) GetServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if f.getServerByVerFn != nil {
		return f.getServerByVerFn(ctx, serverName, "")
	}
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	if f.getProviderByIDFn != nil {
		return f.getProviderByIDFn(ctx, providerID)
	}
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) GetServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.getServerByVerFn != nil {
		return f.getServerByVerFn(ctx, serverName, version)
	}
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) GetServerVersions(context.Context, string) ([]*apiv0.ServerResponse, error) {
	return nil, nil
}

func (f *fakePlatformRuntimeRegistry) PublishServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if req == nil {
		return nil, database.ErrInvalidInput
	}
	return &apiv0.ServerResponse{Server: *req}, nil
}

func (f *fakePlatformRuntimeRegistry) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	_ = serverName
	_ = version
	_ = newStatus
	return f.PublishServer(ctx, req)
}

func (f *fakePlatformRuntimeRegistry) SetServerReadme(context.Context, string, string, []byte, string) error {
	return nil
}

func (f *fakePlatformRuntimeRegistry) GetLatestServerReadme(context.Context, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) GetServerReadme(context.Context, string, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) DeleteServer(context.Context, string, string) error {
	return nil
}

func (f *fakePlatformRuntimeRegistry) SetServerEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (f *fakePlatformRuntimeRegistry) GetServerEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) ListAgents(context.Context, *database.AgentFilter, string, int) ([]*models.AgentResponse, string, error) {
	if f.agentResp != nil {
		return []*models.AgentResponse{f.agentResp}, "", nil
	}
	return nil, "", nil
}

func (f *fakePlatformRuntimeRegistry) GetAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if f.getAgentFn != nil {
		return f.getAgentFn(ctx, agentName, "")
	}
	if f.agentResp != nil {
		return f.agentResp, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.getAgentFn != nil {
		return f.getAgentFn(ctx, agentName, version)
	}
	if f.agentResp != nil {
		return f.agentResp, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) GetAgentVersions(context.Context, string) ([]*models.AgentResponse, error) {
	if f.agentResp != nil {
		return []*models.AgentResponse{f.agentResp}, nil
	}
	return nil, nil
}

func (f *fakePlatformRuntimeRegistry) PublishAgent(context.Context, *models.AgentJSON) (*models.AgentResponse, error) {
	if f.agentResp != nil {
		return f.agentResp, nil
	}
	return nil, database.ErrInvalidInput
}

func (f *fakePlatformRuntimeRegistry) DeleteAgent(context.Context, string, string) error {
	return nil
}

func (f *fakePlatformRuntimeRegistry) SetAgentEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (f *fakePlatformRuntimeRegistry) GetAgentEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakePlatformRuntimeRegistry) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	if f.resolveSkillsFn != nil {
		return f.resolveSkillsFn(ctx, manifest)
	}
	return nil, nil
}

func (f *fakePlatformRuntimeRegistry) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	if f.resolvePromptsFn != nil {
		return f.resolvePromptsFn(ctx, manifest)
	}
	return nil, nil
}

func newPlatformRuntimeServices(registry *fakePlatformRuntimeRegistry) (serversvc.Registry, agentsvc.Registry) {
	return registry, registry
}

func TestSplitDeploymentRuntimeInputs(t *testing.T) {
	envValues, argValues, headerValues := splitDeploymentRuntimeInputs(map[string]string{
		"FOO":                  "bar",
		"ARG_--token":          "abc123",
		"HEADER_Authorization": "Bearer secret",
		"ARG_":                 "ignored",
		"HEADER_":              "ignored",
	})

	if got := envValues["FOO"]; got != "bar" {
		t.Fatalf("env FOO = %q, want %q", got, "bar")
	}
	if got := argValues["--token"]; got != "abc123" {
		t.Fatalf("arg --token = %q, want %q", got, "abc123")
	}
	if got := headerValues["Authorization"]; got != "Bearer secret" {
		t.Fatalf("header Authorization = %q, want %q", got, "Bearer secret")
	}
	if _, ok := argValues[""]; ok {
		t.Fatal("expected empty arg name to be ignored")
	}
	if _, ok := headerValues[""]; ok {
		t.Fatal("expected empty header name to be ignored")
	}
}

func TestTranslateMCPServerRemoteAppliesHeaderOverridesAndDefaults(t *testing.T) {
	server, err := TranslateMCPServer(context.Background(), &MCPServerRunRequest{
		RegistryServer: &apiv0.ServerJSON{
			Name: "remote server",
			Remotes: []model.Transport{{
				URL: "https://example.com/mcp",
				Headers: []model.KeyValueInput{
					{
						Name: "Authorization",
						InputWithVariables: model.InputWithVariables{
							Input: model.Input{IsRequired: true},
						},
					},
					{
						Name: "X-Trace",
						InputWithVariables: model.InputWithVariables{
							Input: model.Input{Default: "trace-default"},
						},
					},
				},
			}},
		},
		HeaderValues: map[string]string{"Authorization": "Bearer token"},
	})
	if err != nil {
		t.Fatalf("TranslateMCPServer() unexpected error: %v", err)
	}
	if server.MCPServerType != "remote" {
		t.Fatalf("MCPServerType = %q, want remote", server.MCPServerType)
	}
	if server.Remote == nil {
		t.Fatal("expected remote config")
	}
	if server.Remote.Host != "example.com" || server.Remote.Port != 443 || server.Remote.Path != "/mcp" {
		t.Fatalf("unexpected remote config: %+v", server.Remote)
	}

	headers := map[string]string{}
	for _, header := range server.Remote.Headers {
		headers[header.Name] = header.Value
	}
	if headers["Authorization"] != "Bearer token" {
		t.Fatalf("Authorization header = %q, want %q", headers["Authorization"], "Bearer token")
	}
	if headers["X-Trace"] != "trace-default" {
		t.Fatalf("X-Trace header = %q, want %q", headers["X-Trace"], "trace-default")
	}
}

func TestTranslateMCPServerLocalIncludesOverridesAndExtraArgs(t *testing.T) {
	server, err := TranslateMCPServer(context.Background(), &MCPServerRunRequest{
		RegistryServer: &apiv0.ServerJSON{
			Name: "test/server",
			Packages: []model.Package{{
				RegistryType: model.RegistryTypeNPM,
				Identifier:   "@test/server",
				Version:      "1.2.3",
				RuntimeArguments: []model.Argument{
					{
						Name: "--token",
						Type: model.ArgumentTypeNamed,
						InputWithVariables: model.InputWithVariables{
							Input: model.Input{Default: "default-token"},
						},
					},
				},
				PackageArguments: []model.Argument{
					{
						Name: "--mode",
						Type: model.ArgumentTypeNamed,
						InputWithVariables: model.InputWithVariables{
							Input: model.Input{Value: "safe"},
						},
					},
				},
				EnvironmentVariables: []model.KeyValueInput{
					{
						Name: "API_KEY",
						InputWithVariables: model.InputWithVariables{
							Input: model.Input{IsRequired: true},
						},
					},
					{
						Name: "OPTIONAL",
						InputWithVariables: model.InputWithVariables{
							Input: model.Input{Default: "fallback"},
						},
					},
				},
				Transport: model.Transport{
					Type: "http",
					URL:  "http://localhost:7777/mcp",
				},
			}},
		},
		EnvValues: map[string]string{"API_KEY": "secret"},
		ArgValues: map[string]string{"--token": "override-token", "--extra": "value"},
	})
	if err != nil {
		t.Fatalf("TranslateMCPServer() unexpected error: %v", err)
	}
	if server.MCPServerType != "local" {
		t.Fatalf("MCPServerType = %q, want local", server.MCPServerType)
	}
	if server.Local == nil {
		t.Fatal("expected local config")
	}
	if server.Local.Deployment.Image != "node:24-alpine3.21" {
		t.Fatalf("image = %q, want node:24-alpine3.21", server.Local.Deployment.Image)
	}
	if server.Local.Deployment.Cmd != "npx" {
		t.Fatalf("cmd = %q, want npx", server.Local.Deployment.Cmd)
	}
	wantArgs := []string{"--token", "override-token", "-y", "@test/server@1.2.3", "--mode", "safe", "--extra", "value"}
	if len(server.Local.Deployment.Args) != len(wantArgs) {
		t.Fatalf("args len = %d, want %d (%v)", len(server.Local.Deployment.Args), len(wantArgs), server.Local.Deployment.Args)
	}
	for i := range wantArgs {
		if server.Local.Deployment.Args[i] != wantArgs[i] {
			t.Fatalf("args[%d] = %q, want %q (all args %v)", i, server.Local.Deployment.Args[i], wantArgs[i], server.Local.Deployment.Args)
		}
	}
	if got := server.Local.Deployment.Env["API_KEY"]; got != "secret" {
		t.Fatalf("API_KEY = %q, want secret", got)
	}
	if got := server.Local.Deployment.Env["OPTIONAL"]; got != "fallback" {
		t.Fatalf("OPTIONAL = %q, want fallback", got)
	}
	if server.Local.HTTP == nil || server.Local.HTTP.Port != 7777 || server.Local.HTTP.Path != "/mcp" {
		t.Fatalf("unexpected HTTP transport: %+v", server.Local.HTTP)
	}
}

func TestResolveAgentDefaultsLocalPort(t *testing.T) {
	registry := &fakePlatformRuntimeRegistry{agentResp: &models.AgentResponse{
		Agent: models.AgentJSON{
			AgentManifest: models.AgentManifest{
				Name:          "planner",
				ModelProvider: "openai",
				ModelName:     "gpt-4o",
			},
			Version: "1.0.0",
		},
	}}
	serverService, agentService := newPlatformRuntimeServices(registry)

	resolved, err := ResolveAgent(context.Background(), serverService, agentService, &models.Deployment{
		ID:         "dep-123",
		ServerName: "planner",
		Version:    "1.0.0",
		Env:        map[string]string{},
	}, "")
	if err != nil {
		t.Fatalf("ResolveAgent() unexpected error: %v", err)
	}
	if resolved.Agent.Deployment.Port != DefaultLocalAgentPort {
		t.Fatalf("port = %d, want %d", resolved.Agent.Deployment.Port, DefaultLocalAgentPort)
	}
}

func TestResolveAgentNamespaceDefaulting(t *testing.T) {
	newRegistry := func() *fakePlatformRuntimeRegistry {
		return &fakePlatformRuntimeRegistry{agentResp: &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:          "planner",
					ModelProvider: "openai",
					ModelName:     "gpt-4o",
				},
				Version: "1.0.0",
			},
		}}
	}

	tests := []struct {
		name          string
		namespace     string
		deploymentEnv map[string]string
		wantNamespace string
	}{
		{
			name:          "defaults to 'default' when namespace param is empty",
			namespace:     "",
			deploymentEnv: map[string]string{},
			wantNamespace: "default",
		},
		{
			name:          "uses explicit namespace param",
			namespace:     "production",
			deploymentEnv: map[string]string{},
			wantNamespace: "production",
		},
		{
			name:          "deployment env KAGENT_NAMESPACE takes priority over namespace param",
			namespace:     "staging",
			deploymentEnv: map[string]string{"KAGENT_NAMESPACE": "from-env"},
			wantNamespace: "from-env",
		},
		{
			name:          "deployment env KAGENT_NAMESPACE takes priority over default",
			namespace:     "",
			deploymentEnv: map[string]string{"KAGENT_NAMESPACE": "from-env"},
			wantNamespace: "from-env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registry := newRegistry()
			serverService, agentService := newPlatformRuntimeServices(registry)
			resolved, err := ResolveAgent(context.Background(), serverService, agentService, &models.Deployment{
				ID:         "dep-123",
				ServerName: "planner",
				Version:    "1.0.0",
				Env:        tt.deploymentEnv,
			}, tt.namespace)
			if err != nil {
				t.Fatalf("ResolveAgent() unexpected error: %v", err)
			}
			got := resolved.Agent.Deployment.Env["KAGENT_NAMESPACE"]
			if got != tt.wantNamespace {
				t.Errorf("KAGENT_NAMESPACE = %q, want %q", got, tt.wantNamespace)
			}
		})
	}
}

func TestBuildRemoteMCPURL(t *testing.T) {
	tests := []struct {
		name   string
		remote *platformtypes.RemoteMCPServer
		want   string
	}{
		{"https standard port", &platformtypes.RemoteMCPServer{Scheme: "https", Host: "example.com", Port: 443, Path: "/mcp"}, "https://example.com/mcp"},
		{"https custom port", &platformtypes.RemoteMCPServer{Scheme: "https", Host: "example.com", Port: 8443, Path: "/mcp"}, "https://example.com:8443/mcp"},
		{"http standard port", &platformtypes.RemoteMCPServer{Scheme: "http", Host: "example.com", Port: 80, Path: "/sse"}, "http://example.com/sse"},
		{"http custom port", &platformtypes.RemoteMCPServer{Scheme: "http", Host: "localhost", Port: 3005, Path: "/mcp/"}, "http://localhost:3005/mcp/"},
		{"empty path", &platformtypes.RemoteMCPServer{Scheme: "https", Host: "example.com", Port: 443, Path: ""}, "https://example.com"},
		{"empty scheme defaults to http", &platformtypes.RemoteMCPServer{Host: "example.com", Port: 80, Path: "/mcp"}, "http://example.com/mcp"},
		{"ipv6 with custom port", &platformtypes.RemoteMCPServer{Scheme: "http", Host: "::1", Port: 3005, Path: "/mcp"}, "http://[::1]:3005/mcp"},
		{"ipv6 standard port", &platformtypes.RemoteMCPServer{Scheme: "https", Host: "::1", Port: 443, Path: "/mcp"}, "https://[::1]/mcp"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BuildRemoteMCPURL(tt.remote); got != tt.want {
				t.Errorf("BuildRemoteMCPURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		want    parsedURL
		wantErr bool
	}{
		{"https with explicit port", "https://example.com:8443/mcp", parsedURL{scheme: "https", host: "example.com", port: 8443, path: "/mcp"}, false},
		{"https default port", "https://example.com/mcp", parsedURL{scheme: "https", host: "example.com", port: 443, path: "/mcp"}, false},
		{"http default port", "http://example.com/sse", parsedURL{scheme: "http", host: "example.com", port: 80, path: "/sse"}, false},
		{"http with explicit port", "http://localhost:3005/mcp", parsedURL{scheme: "http", host: "localhost", port: 3005, path: "/mcp"}, false},
		{"no path", "https://example.com", parsedURL{scheme: "https", host: "example.com", port: 443, path: ""}, false},
		{"ipv6 with port", "http://[::1]:3005/mcp", parsedURL{scheme: "http", host: "::1", port: 3005, path: "/mcp"}, false},
		{"ipv6 without port", "https://[::1]/mcp", parsedURL{scheme: "https", host: "::1", port: 443, path: "/mcp"}, false},
		{"invalid port", "http://example.com:notaport/mcp", parsedURL{}, true},
		{"empty scheme", "://example.com/mcp", parsedURL{}, true},
		{"unsupported scheme", "ftp://example.com/mcp", parsedURL{}, true},
		{"no scheme", "example.com/mcp", parsedURL{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseURL(tt.rawURL)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseURL(%q) error = %v, wantErr %v", tt.rawURL, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if *got != tt.want {
				t.Errorf("parseURL(%q) = %+v, want %+v", tt.rawURL, *got, tt.want)
			}
		})
	}
}
