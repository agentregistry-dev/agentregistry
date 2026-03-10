package utils

import (
	"context"
	"testing"

	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

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
	registry := servicetesting.NewFakeRegistry()
	registry.Agents = []*models.AgentResponse{{
		Agent: models.AgentJSON{
			AgentManifest: models.AgentManifest{
				Name:          "planner",
				ModelProvider: "openai",
				ModelName:     "gpt-4o",
			},
			Version: "1.0.0",
		},
	}}

	resolved, err := ResolveAgent(context.Background(), registry, &models.Deployment{
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
