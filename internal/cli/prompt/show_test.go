package prompt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestRunShow_NilClient(t *testing.T) {
	oldClient := apiClient
	apiClient = nil
	defer func() { apiClient = oldClient }()

	err := runShow(ShowCmd, []string{"some-prompt"})
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
	}
	if err.Error() != "API client not initialized" {
		t.Errorf("expected 'API client not initialized', got %q", err.Error())
	}
}

func TestRunShow_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	err := runShow(ShowCmd, []string{"nonexistent-prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunShow_Table(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/prompts/") {
			resp := models.PromptResponse{
				Prompt: models.PromptJSON{
					Name:        "test-prompt",
					Version:     "1.0.0",
					Description: "A test prompt",
					Content:     "You are a helpful assistant.",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	oldFormat := showOutputFormat
	showOutputFormat = "table"
	defer func() { showOutputFormat = oldFormat }()

	err := runShow(ShowCmd, []string{"test-prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunShow_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/prompts/") {
			resp := models.PromptResponse{
				Prompt: models.PromptJSON{
					Name:        "test-prompt",
					Version:     "1.0.0",
					Description: "A test prompt",
					Content:     "You are a helpful assistant.",
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	oldFormat := showOutputFormat
	showOutputFormat = "json"
	defer func() { showOutputFormat = oldFormat }()

	err := runShow(ShowCmd, []string{"test-prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunShow_LongContentTruncated(t *testing.T) {
	longContent := strings.Repeat("a", 300)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := models.PromptResponse{
			Prompt: models.PromptJSON{
				Name:        "long-prompt",
				Version:     "1.0.0",
				Description: "Prompt with long content",
				Content:     longContent,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	oldFormat := showOutputFormat
	showOutputFormat = "table"
	defer func() { showOutputFormat = oldFormat }()

	// Should not error — truncation happens silently.
	err := runShow(ShowCmd, []string{"long-prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunShow_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	err := runShow(ShowCmd, []string{"error-prompt"})
	if err == nil {
		t.Fatal("expected error from API failure, got nil")
	}
}
