package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

func TestStatusCmd_Reachable(t *testing.T) {
	// Create a mock server that responds to /ping and /version
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping":
			w.WriteHeader(http.StatusOK)
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{
				"version":    "0.3.2",
				"git_commit": "abc123",
				"build_time": "2026-03-30",
			})
		default:
			// Return empty arrays for list endpoints
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer srv.Close()

	// Set the API client to use the mock server
	apiClient = client.NewClient(srv.URL, "")
	defer func() { apiClient = nil }()

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	StatusCmd.Run(StatusCmd, []string{})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if len(output) == 0 {
		t.Error("Expected non-empty output from status command")
	}
}

func TestStatusCmd_Unreachable(t *testing.T) {
	// Point at a non-existent server
	apiClient = client.NewClient("http://127.0.0.1:1", "")
	defer func() { apiClient = nil }()

	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	StatusCmd.Run(StatusCmd, []string{})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	if len(output) == 0 {
		t.Error("Expected non-empty output from status command")
	}
}

func TestStatusCmd_JSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ping":
			w.WriteHeader(http.StatusOK)
		case "/version":
			json.NewEncoder(w).Encode(map[string]string{
				"version": "0.3.2",
			})
		default:
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode([]interface{}{})
		}
	}))
	defer srv.Close()

	apiClient = client.NewClient(srv.URL, "")
	defer func() { apiClient = nil }()

	// Set JSON flag
	statusJSON = true
	defer func() { statusJSON = false }()

	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	StatusCmd.Run(StatusCmd, []string{})

	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)

	var result statusResult
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("Expected valid JSON output, got error: %v\nOutput: %s", err, buf.String())
	}
	if !result.Registry.Reachable {
		t.Error("Expected registry to be reachable")
	}
}
