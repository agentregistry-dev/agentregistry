package configure

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

// runConfigure executes the configure command in the current working directory
// and returns its combined output.
func runConfigure(t *testing.T, args ...string) string {
	t.Helper()
	cmd := NewCommand(cliruntime.Deps{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("configure %v: %v", args, err)
	}
	return out.String()
}

// seedFile writes content at path relative to the current working directory,
// creating parent directories.
func seedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("creating seed directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing seed file: %v", err)
	}
}

// assertFileJSON compares the JSON content of path against want, ignoring
// formatting and key order.
func assertFileJSON(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var got, expected any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parsing %s: %v\ncontent:\n%s", path, err, data)
	}
	if err := json.Unmarshal([]byte(want), &expected); err != nil {
		t.Fatalf("parsing expected JSON: %v", err)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("file %s content mismatch\ngot:\n%s\nwant:\n%s", path, data, want)
	}
}

func TestConfigure(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		configPath string
		seed       string
		runTwice   bool
		want       string
		wantOutput []string
	}{
		{
			name:       "claude-code fresh config",
			args:       []string{"claude-code"},
			configPath: ".mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {"type": "http", "url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "vscode fresh config",
			args:       []string{"vscode"},
			configPath: ".vscode/mcp.json",
			want: `{
				"servers": {
					"arctl": {"type": "http", "url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "cursor fresh config",
			args:       []string{"cursor"},
			configPath: ".cursor/mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {"url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "kiro fresh config",
			args:       []string{"kiro"},
			configPath: ".kiro/settings/mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {"url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "custom url without port",
			args:       []string{"claude-code", "--url", "https://are.example.dev/mcp"},
			configPath: ".mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {"type": "http", "url": "https://are.example.dev/mcp"}
				}
			}`,
		},
		{
			name:       "custom port",
			args:       []string{"cursor", "--port", "9999"},
			configPath: ".cursor/mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {"url": "http://localhost:9999/mcp"}
				}
			}`,
		},
		{
			name:       "cursor merge preserves stdio server and unknown fields",
			args:       []string{"cursor"},
			configPath: ".cursor/mcp.json",
			seed: `{
				"mcpServers": {
					"other-server": {
						"command": "npx",
						"args": ["-y", "mcp-server"],
						"env": {"API_KEY": "value"},
						"disabled": false
					}
				}
			}`,
			want: `{
				"mcpServers": {
					"other-server": {
						"command": "npx",
						"args": ["-y", "mcp-server"],
						"env": {"API_KEY": "value"},
						"disabled": false
					},
					"arctl": {"url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "kiro merge preserves autoApprove and headers",
			args:       []string{"kiro"},
			configPath: ".kiro/settings/mcp.json",
			seed: `{
				"mcpServers": {
					"other-server": {
						"url": "https://other.example.com/mcp",
						"headers": {"Authorization": "Bearer token"},
						"autoApprove": ["some_tool"]
					}
				}
			}`,
			want: `{
				"mcpServers": {
					"other-server": {
						"url": "https://other.example.com/mcp",
						"headers": {"Authorization": "Bearer token"},
						"autoApprove": ["some_tool"]
					},
					"arctl": {"url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "vscode merge preserves unknown top-level keys and existing servers",
			args:       []string{"vscode"},
			configPath: ".vscode/mcp.json",
			seed: `{
				"inputs": [
					{"type": "promptString", "id": "other-token", "description": "Other token", "password": true}
				],
				"servers": {
					"other-server": {
						"type": "stdio",
						"command": "npx",
						"args": ["-y", "mcp-server"]
					}
				}
			}`,
			want: `{
				"inputs": [
					{"type": "promptString", "id": "other-token", "description": "Other token", "password": true}
				],
				"servers": {
					"other-server": {
						"type": "stdio",
						"command": "npx",
						"args": ["-y", "mcp-server"]
					},
					"arctl": {"type": "http", "url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "claude-code merge preserves headers and timeout on existing servers",
			args:       []string{"claude-code"},
			configPath: ".mcp.json",
			seed: `{
				"mcpServers": {
					"other-server": {
						"type": "http",
						"url": "https://other.example.com/mcp",
						"headers": {"Authorization": "Bearer ${OTHER_TOKEN}"},
						"timeout": 30000
					}
				}
			}`,
			want: `{
				"mcpServers": {
					"other-server": {
						"type": "http",
						"url": "https://other.example.com/mcp",
						"headers": {"Authorization": "Bearer ${OTHER_TOKEN}"},
						"timeout": 30000
					},
					"arctl": {"type": "http", "url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "legacy ARCTL entry is migrated",
			args:       []string{"cursor"},
			configPath: ".cursor/mcp.json",
			seed: `{
				"mcpServers": {
					"ARCTL": {"url": "http://localhost:21212/mcp"}
				}
			}`,
			want: `{
				"mcpServers": {
					"arctl": {"url": "http://localhost:21212/mcp"}
				}
			}`,
		},
		{
			name:       "claude-code token emission",
			args:       []string{"claude-code", "--token-env", "ARCTL_MCP_TOKEN"},
			configPath: ".mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {
						"type": "http",
						"url": "http://localhost:21212/mcp",
						"headers": {"Authorization": "Bearer ${ARCTL_MCP_TOKEN}"}
					}
				}
			}`,
		},
		{
			name:       "cursor token emission",
			args:       []string{"cursor", "--token-env", "ARCTL_MCP_TOKEN"},
			configPath: ".cursor/mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {
						"url": "http://localhost:21212/mcp",
						"headers": {"Authorization": "Bearer ${env:ARCTL_MCP_TOKEN}"}
					}
				}
			}`,
		},
		{
			name:       "kiro token emission",
			args:       []string{"kiro", "--token-env", "ARCTL_MCP_TOKEN"},
			configPath: ".kiro/settings/mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {
						"url": "http://localhost:21212/mcp",
						"headers": {"Authorization": "Bearer ${env:ARCTL_MCP_TOKEN}"}
					}
				}
			}`,
		},
		{
			name:       "vscode token emission adds inputs entry",
			args:       []string{"vscode", "--token-env", "ARCTL_MCP_TOKEN"},
			configPath: ".vscode/mcp.json",
			want: `{
				"servers": {
					"arctl": {
						"type": "http",
						"url": "http://localhost:21212/mcp",
						"headers": {"Authorization": "Bearer ${input:arctl-mcp-token}"}
					}
				},
				"inputs": [
					{"type": "promptString", "id": "arctl-mcp-token", "description": "Bearer token for the arctl MCP server", "password": true}
				]
			}`,
			wantOutput: []string{"Note: VS Code prompts for the token on first connection"},
		},
		{
			name:       "vscode token emission appends to existing inputs",
			args:       []string{"vscode", "--token-env", "ARCTL_MCP_TOKEN"},
			configPath: ".vscode/mcp.json",
			seed: `{
				"inputs": [
					{"type": "promptString", "id": "other-token", "description": "Other token", "password": true}
				],
				"servers": {
					"other-server": {"type": "http", "url": "https://other.example.com/mcp"}
				}
			}`,
			want: `{
				"inputs": [
					{"type": "promptString", "id": "other-token", "description": "Other token", "password": true},
					{"type": "promptString", "id": "arctl-mcp-token", "description": "Bearer token for the arctl MCP server", "password": true}
				],
				"servers": {
					"other-server": {"type": "http", "url": "https://other.example.com/mcp"},
					"arctl": {
						"type": "http",
						"url": "http://localhost:21212/mcp",
						"headers": {"Authorization": "Bearer ${input:arctl-mcp-token}"}
					}
				}
			}`,
		},
		{
			name:       "vscode token emission is idempotent",
			args:       []string{"vscode", "--token-env", "ARCTL_MCP_TOKEN"},
			configPath: ".vscode/mcp.json",
			runTwice:   true,
			want: `{
				"servers": {
					"arctl": {
						"type": "http",
						"url": "http://localhost:21212/mcp",
						"headers": {"Authorization": "Bearer ${input:arctl-mcp-token}"}
					}
				},
				"inputs": [
					{"type": "promptString", "id": "arctl-mcp-token", "description": "Bearer token for the arctl MCP server", "password": true}
				]
			}`,
		},
		{
			name:       "port is ignored with url",
			args:       []string{"claude-code", "--url", "https://example.com/mcp", "--port", "9999"},
			configPath: ".mcp.json",
			want: `{
				"mcpServers": {
					"arctl": {"type": "http", "url": "https://example.com/mcp"}
				}
			}`,
			wantOutput: []string{"Warning: --port is ignored when --url is set"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Chdir(t.TempDir())

			if tt.seed != "" {
				seedFile(t, tt.configPath, tt.seed)
			}

			output := runConfigure(t, tt.args...)
			if tt.runTwice {
				output = runConfigure(t, tt.args...)
			}

			assertFileJSON(t, tt.configPath, tt.want)
			for _, want := range tt.wantOutput {
				if !strings.Contains(output, want) {
					t.Errorf("output missing %q\ngot:\n%s", want, output)
				}
			}
		})
	}
}

func TestConfigure_ListsSupportedClients(t *testing.T) {
	t.Chdir(t.TempDir())
	output := runConfigure(t)
	for _, client := range []string{"vscode", "cursor", "claude-code", "kiro"} {
		if !strings.Contains(output, client) {
			t.Errorf("client listing missing %q\ngot:\n%s", client, output)
		}
	}
}

func TestConfigure_UnsupportedClient(t *testing.T) {
	t.Chdir(t.TempDir())
	cmd := NewCommand(cliruntime.Deps{})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"not-a-client"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for unsupported client")
	}
}
