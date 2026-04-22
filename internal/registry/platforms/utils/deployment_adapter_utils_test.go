package utils

import (
	"context"
	"testing"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestTranslateMCPServerRemoteAppliesHeaderOverridesAndDefaults(t *testing.T) {
	server, err := TranslateMCPServer(context.Background(), &MCPServerRunRequest{
		Name: "remote server",
		Spec: v1alpha1.MCPServerSpec{
			Remotes: []v1alpha1.MCPTransport{{
				URL: "https://example.com/mcp",
				Headers: []v1alpha1.MCPKeyValueInput{
					{Name: "Authorization", IsRequired: true},
					{Name: "X-Trace", Default: "trace-default"},
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
		Name: "test/server",
		Spec: v1alpha1.MCPServerSpec{
			Packages: []v1alpha1.MCPPackage{{
				RegistryType: v1alpha1.RegistryTypeNPM,
				Identifier:   "@test/server",
				Version:      "1.2.3",
				RuntimeArguments: []v1alpha1.MCPArgument{{
					Name:    "--token",
					Type:    v1alpha1.MCPArgumentTypeNamed,
					Default: "default-token",
				}},
				PackageArguments: []v1alpha1.MCPArgument{{
					Name:  "--mode",
					Type:  v1alpha1.MCPArgumentTypeNamed,
					Value: "safe",
				}},
				EnvironmentVariables: []v1alpha1.MCPKeyValueInput{
					{Name: "API_KEY", IsRequired: true},
					{Name: "OPTIONAL", Default: "fallback"},
				},
				Transport: v1alpha1.MCPTransport{
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
