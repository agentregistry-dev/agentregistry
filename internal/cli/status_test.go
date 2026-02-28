package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestStatusCmd_DaemonStopped(t *testing.T) {
	// Point to a non-existent server so Ping fails.
	t.Setenv("ARCTL_API_BASE_URL", "http://127.0.0.1:19999/v0")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := StatusCmd.RunE(StatusCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "stopped") {
		t.Errorf("expected 'stopped' in output, got: %s", out)
	}
	if !strings.Contains(out, "unreachable") {
		t.Errorf("expected 'unreachable' in output, got: %s", out)
	}
}

func TestStatusCmd_DaemonRunning(t *testing.T) {
	// Start a mock server that responds to /v0/ping, /v0/version, /v0/servers, /v0/agents, /v0/skills
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"pong":true}`))
	})
	mux.HandleFunc("/v0/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"version":    "v0.5.0",
			"git_commit": "abc1234",
			"build_time": "2026-02-08T00:00:00Z",
		})
	})
	mux.HandleFunc("/v0/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"servers":  []any{map[string]any{"name": "test-server"}},
			"metadata": map[string]string{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/v0/agents", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"agents":   []any{map[string]any{"name": "test-agent"}, map[string]any{"name": "test-agent-2"}},
			"metadata": map[string]string{"next_cursor": ""},
		})
	})
	mux.HandleFunc("/v0/skills", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"skills":   []any{},
			"metadata": map[string]string{"next_cursor": ""},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	t.Setenv("ARCTL_API_BASE_URL", srv.URL+"/v0")

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := StatusCmd.RunE(StatusCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	out := buf.String()

	if !strings.Contains(out, "running") {
		t.Errorf("expected 'running' in output, got: %s", out)
	}
	if !strings.Contains(out, "v0.5.0") {
		t.Errorf("expected server version in output, got: %s", out)
	}
	if !strings.Contains(out, "MCP servers:     1") {
		t.Errorf("expected 1 server in output, got: %s", out)
	}
	if !strings.Contains(out, "Agents:          2") {
		t.Errorf("expected 2 agents in output, got: %s", out)
	}
	if !strings.Contains(out, "Skills:          0") {
		t.Errorf("expected 0 skills in output, got: %s", out)
	}
}

func TestStatusCmd_JSONOutput(t *testing.T) {
	// Point to a non-existent server.
	t.Setenv("ARCTL_API_BASE_URL", "http://127.0.0.1:19999/v0")

	// Set JSON output
	statusOutputFormat = "json"
	defer func() { statusOutputFormat = "table" }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := StatusCmd.RunE(StatusCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)

	var info statusInfo
	if err := json.Unmarshal(buf.Bytes(), &info); err != nil {
		t.Fatalf("expected valid JSON, got parse error: %v\noutput: %s", err, buf.String())
	}
	if info.Daemon != "stopped" {
		t.Errorf("expected daemon=stopped, got %s", info.Daemon)
	}
	if info.API != "unreachable" {
		t.Errorf("expected api=unreachable, got %s", info.API)
	}
}
