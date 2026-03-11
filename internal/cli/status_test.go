package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

func TestStatusCmd_Metadata(t *testing.T) {
	if StatusCmd.Use != "status" {
		t.Errorf("StatusCmd.Use = %q, want %q", StatusCmd.Use, "status")
	}
	if StatusCmd.Short == "" {
		t.Error("StatusCmd.Short is empty")
	}
}

func TestCheckDaemonHealth_Healthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v0/ping" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	result := checkDaemonHealth(ts.URL + "/v0")
	if !result.healthy {
		t.Errorf("expected healthy=true, got false (err=%s)", result.err)
	}
}

func TestCheckDaemonHealth_Unhealthy(t *testing.T) {
	// Use a server that always returns 500
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	result := checkDaemonHealth(ts.URL + "/v0")
	if result.healthy {
		t.Error("expected healthy=false, got true")
	}
}

func TestCheckDaemonHealth_Unreachable(t *testing.T) {
	result := checkDaemonHealth("http://localhost:19999/v0")
	if result.healthy {
		t.Error("expected healthy=false for unreachable server, got true")
	}
}

func TestRunStatus_DaemonDown(t *testing.T) {
	apiClient = nil

	// Should not error; prints "not running" and returns nil
	err := runStatus()
	if err != nil {
		t.Errorf("runStatus() returned error for unreachable daemon: %v", err)
	}
}

func TestRunStatus_FullStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v0/ping":
			w.WriteHeader(http.StatusOK)
		case "/v0/version":
			json.NewEncoder(w).Encode(map[string]string{
				"version":   "1.0.0",
				"gitCommit": "abc123",
				"buildTime": "2026-01-01",
			})
		case "/v0/servers":
			json.NewEncoder(w).Encode(map[string]any{
				"servers":  []any{},
				"metadata": map[string]string{"next_cursor": ""},
			})
		case "/v0/agents":
			json.NewEncoder(w).Encode(map[string]any{
				"agents":   []any{},
				"metadata": map[string]string{"next_cursor": ""},
			})
		case "/v0/skills":
			json.NewEncoder(w).Encode(map[string]any{
				"skills":   []any{},
				"metadata": map[string]string{"next_cursor": ""},
			})
		case "/v0/prompts":
			json.NewEncoder(w).Encode(map[string]any{
				"prompts":  []any{},
				"metadata": map[string]string{"next_cursor": ""},
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	apiClient = client.NewClient(ts.URL, "")
	err := runStatus()
	if err != nil {
		t.Errorf("runStatus() returned error: %v", err)
	}
}
