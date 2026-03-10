//go:build e2e

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
)

// TestMCPPublishRemoteURL tests publishing a remote-only MCP server with --remote-url
// and verifies the URL (including scheme) is correctly stored in the registry.
func TestMCPPublishRemoteURL(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	tests := []struct {
		name              string
		namespace         string
		remoteURLFmt      string
		transport         string
		expectedTransport string
	}{
		{
			name:              "https url with default transport",
			namespace:         "com.databricks",
			remoteURLFmt:      "https://my-workspace.cloud.databricks.com/%s",
			expectedTransport: "streamable-http",
		},
		{
			name:              "http url with explicit sse transport",
			namespace:         "com.example",
			remoteURLFmt:      "http://example.com:8080/%s",
			transport:         "sse",
			expectedTransport: "sse",
		},
		{
			name:              "https url with explicit streamable-http",
			namespace:         "com.example",
			remoteURLFmt:      "https://remote.example.com/api/%s",
			transport:         "streamable-http",
			expectedTransport: "streamable-http",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mcpName := UniqueNameWithPrefix("e2e-remote")
			serverName := tt.namespace + "/" + mcpName
			remoteURL := fmt.Sprintf(tt.remoteURLFmt, mcpName)
			version := "0.0.1-e2e"

			args := []string{
				"mcp", "publish", serverName,
				"--remote-url", remoteURL,
				"--version", version,
				"--description", "E2E remote-url test",
				"--registry-url", regURL,
			}
			if tt.transport != "" {
				args = append(args, "--transport", tt.transport)
			}

			t.Run("publish", func(t *testing.T) {
				result := RunArctl(t, tmpDir, args...)
				RequireSuccess(t, result)
			})

			t.Run("verify_remote_url_stored", func(t *testing.T) {
				result := RunArctl(t, tmpDir,
					"mcp", "show", serverName,
					"--version", version,
					"--output", "json",
					"--registry-url", regURL,
				)
				RequireSuccess(t, result)
				RequireOutputContains(t, result, remoteURL)
				RequireOutputContains(t, result, tt.expectedTransport)
			})

			t.Run("verify_via_api", func(t *testing.T) {
				resp := RegistryGet(t, regURL+"/servers/"+url.PathEscape(serverName)+"/versions/"+version)
				defer resp.Body.Close()

				if resp.StatusCode != http.StatusOK {
					t.Fatalf("Expected 200, got %d", resp.StatusCode)
				}

				var body struct {
					Servers []struct {
						Server struct {
							Remotes []struct {
								URL  string `json:"url"`
								Type string `json:"type"`
							} `json:"remotes"`
							Packages []json.RawMessage `json:"packages"`
						} `json:"server"`
					} `json:"servers"`
				}
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("Failed to decode response: %v", err)
				}

				if len(body.Servers) != 1 {
					t.Fatalf("Expected 1 server in response, got %d", len(body.Servers))
				}

				server := body.Servers[0].Server
				if len(server.Packages) != 0 {
					t.Errorf("Expected no packages for remote-only server, got %d", len(server.Packages))
				}

				if len(server.Remotes) != 1 {
					t.Fatalf("Expected 1 remote entry, got %d", len(server.Remotes))
				}

				remote := server.Remotes[0]
				if remote.URL != remoteURL {
					t.Errorf("Remote URL = %q, want %q", remote.URL, remoteURL)
				}
				if remote.Type != tt.expectedTransport {
					t.Errorf("Remote transport = %q, want %q", remote.Type, tt.expectedTransport)
				}
			})
		})
	}
}

// TestMCPPublishRemoteURLDryRun verifies that --dry-run with --remote-url
// outputs the correct JSON without actually publishing.
func TestMCPPublishRemoteURLDryRun(t *testing.T) {
	tmpDir := t.TempDir()
	regURL := RegistryURL(t)

	serverName := "com.example/" + UniqueNameWithPrefix("e2e-dryrun")
	result := RunArctl(t, tmpDir,
		"mcp", "publish", serverName,
		"--remote-url", "https://secure.example.com/mcp",
		"--version", "1.0.0",
		"--description", "Dry run test",
		"--dry-run",
		"--registry-url", regURL,
	)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "DRY RUN")
	RequireOutputContains(t, result, "https://secure.example.com/mcp")
	RequireOutputContains(t, result, "streamable-http")
}

// TestMCPPublishRemoteURLValidation verifies flag conflicts and invalid
// combinations when using --remote-url.
func TestMCPPublishRemoteURLValidation(t *testing.T) {
	tmpDir := t.TempDir()
	regURL := RegistryURL(t)

	t.Run("conflicts_with_type", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "publish", "e2e-test/conflict-type",
			"--remote-url", "https://example.com/mcp",
			"--type", "oci",
			"--version", "0.0.1",
			"--description", "conflict test",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "--type cannot be used with --remote-url")
	})

	t.Run("conflicts_with_package_id", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "publish", "e2e-test/conflict-pkg",
			"--remote-url", "https://example.com/mcp",
			"--package-id", "docker.io/test/server:latest",
			"--version", "0.0.1",
			"--description", "conflict test",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "--package-id cannot be used with --remote-url")
	})

	t.Run("invalid_transport_type", func(t *testing.T) {
		result := RunArctl(t, tmpDir,
			"mcp", "publish", "e2e-test/invalid-transport",
			"--remote-url", "https://example.com/mcp",
			"--transport", "stdio",
			"--version", "0.0.1",
			"--description", "invalid transport test",
			"--registry-url", regURL,
		)
		RequireFailure(t, result)
		RequireOutputContains(t, result, "--transport must be")
	})
}
