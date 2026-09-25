package bundle

import "testing"

func TestValidAgentPluginsServer(t *testing.T) {
	tests := []struct {
		name   string
		server string
		want   bool
	}{
		{"bare stdio command", `{"type":"stdio","command":"npx","args":["-y","x"],"env":{"A":"b"}}`, true},
		{"plugin-relative command", `{"type":"stdio","command":"./bin/server"}`, true},
		{"cwd inside the plugin", `{"type":"stdio","command":"x","cwd":"./data/../work"}`, true},
		{"cwd in the data directory", `{"type":"stdio","command":"x","cwd":"${PLUGIN_DATA}/cache"}`, true},
		{"https remote", `{"type":"streamable-http","url":"https://example.com/mcp","headers":{"A":"b"}}`, true},
		{"http loopback sse", `{"type":"sse","url":"http://127.0.0.1:8080/sse"}`, true},
		{"http localhost", `{"type":"sse","url":"http://localhost/sse"}`, true},
		{"empty entry", `{}`, false},
		{"null entry", `null`, false},
		{"unknown transport", `{"type":"websocket","url":"https://example.com"}`, false},
		{"unknown field", `{"type":"stdio","command":"x","timeout":5}`, false},
		{"field with wrong type", `{"type":"stdio","command":"x","args":"y"}`, false},
		{"stdio without command", `{"type":"stdio"}`, false},
		{"stdio command with a space", `{"type":"stdio","command":"npx -y x"}`, false},
		{"stdio command with a path", `{"type":"stdio","command":"bin/server"}`, false},
		{"stdio absolute command", `{"type":"stdio","command":"/usr/bin/server"}`, false},
		{"stdio command with ..", `{"type":"stdio","command":"./bin/../server"}`, false},
		{"stdio with url", `{"type":"stdio","command":"x","url":"https://example.com"}`, false},
		{"stdio with headers", `{"type":"stdio","command":"x","headers":{}}`, false},
		{"stdio overrides PLUGIN_ROOT", `{"type":"stdio","command":"x","env":{"PLUGIN_ROOT":"/"}}`, false},
		{"stdio overrides PLUGIN_DATA", `{"type":"stdio","command":"x","env":{"PLUGIN_DATA":"/"}}`, false},
		{"cwd escapes the plugin", `{"type":"stdio","command":"x","cwd":"./../other"}`, false},
		{"cwd variable escapes", `{"type":"stdio","command":"x","cwd":"${PLUGIN_ROOT}/../other"}`, false},
		{"cwd variable glued to a name", `{"type":"stdio","command":"x","cwd":"${PLUGIN_ROOT}x"}`, false},
		{"absolute cwd", `{"type":"stdio","command":"x","cwd":"/tmp"}`, false},
		{"remote without url", `{"type":"sse"}`, false},
		{"remote with command", `{"type":"sse","url":"https://example.com","command":"x"}`, false},
		{"remote with empty args", `{"type":"sse","url":"https://example.com","args":[]}`, false},
		{"remote with env", `{"type":"sse","url":"https://example.com","env":{}}`, false},
		{"remote with cwd", `{"type":"sse","url":"https://example.com","cwd":"./x"}`, false},
		{"http to a public host", `{"type":"sse","url":"http://example.com/sse"}`, false},
		{"url with credentials", `{"type":"sse","url":"https://u:p@example.com"}`, false},
		{"url with fragment", `{"type":"sse","url":"https://example.com/#x"}`, false},
		{"url without host", `{"type":"sse","url":"https:///mcp"}`, false},
		{"url with other scheme", `{"type":"sse","url":"ftp://example.com"}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validAgentPluginsServer([]byte(tt.server)); got != tt.want {
				t.Fatalf("validAgentPluginsServer(%s) = %v, want %v", tt.server, got, tt.want)
			}
		})
	}
}
