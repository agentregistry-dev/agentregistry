package prompt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestRunList_NilClient(t *testing.T) {
	oldClient := apiClient
	apiClient = nil
	defer func() { apiClient = oldClient }()

	err := runList(ListCmd, nil)
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
	}
	if err.Error() != "API client not initialized" {
		t.Errorf("expected 'API client not initialized', got %q", err.Error())
	}
}

func TestRunList_EmptyList(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := models.PromptListResponse{
			Prompts:  []models.PromptResponse{},
			Metadata: models.PromptMetadata{Count: 0},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	err := runList(ListCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunList_WithPrompts_Table(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := models.PromptListResponse{
			Prompts: []models.PromptResponse{
				{
					Prompt: models.PromptJSON{
						Name:        "prompt-one",
						Version:     "1.0.0",
						Description: "First prompt",
						Content:     "Content one",
					},
				},
				{
					Prompt: models.PromptJSON{
						Name:        "prompt-two",
						Version:     "2.0.0",
						Description: "Second prompt",
						Content:     "Content two",
					},
				},
			},
			Metadata: models.PromptMetadata{Count: 2},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	oldFormat := outputFormat
	outputFormat = "table"
	listAll = true
	defer func() {
		outputFormat = oldFormat
		listAll = false
	}()

	err := runList(ListCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunList_WithPrompts_JSON(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := models.PromptListResponse{
			Prompts: []models.PromptResponse{
				{
					Prompt: models.PromptJSON{
						Name:        "prompt-one",
						Version:     "1.0.0",
						Description: "First prompt",
						Content:     "Content one",
					},
				},
			},
			Metadata: models.PromptMetadata{Count: 1},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	oldFormat := outputFormat
	outputFormat = "json"
	defer func() { outputFormat = oldFormat }()

	err := runList(ListCmd, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunList_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	err := runList(ListCmd, nil)
	if err == nil {
		t.Fatal("expected error from API failure, got nil")
	}
}

func TestPrintPromptsTable(t *testing.T) {
	// Smoke test — should not panic with valid data.
	prompts := []*models.PromptResponse{
		{
			Prompt: models.PromptJSON{
				Name:        "a-prompt",
				Version:     "0.1.0",
				Description: "Short desc",
			},
		},
	}
	printPromptsTable(prompts)
}

func TestPrintPromptsTable_Empty(t *testing.T) {
	// Should not panic with an empty slice.
	printPromptsTable([]*models.PromptResponse{})
}

func TestDisplayPaginatedPrompts_ShowAll(t *testing.T) {
	prompts := make([]*models.PromptResponse, 20)
	for i := range prompts {
		prompts[i] = &models.PromptResponse{
			Prompt: models.PromptJSON{
				Name:    "prompt",
				Version: "1.0.0",
			},
		}
	}

	// showAll=true should print all without pagination.
	// This just verifies it doesn't panic.
	displayPaginatedPrompts(prompts, 5, true)
}

func TestDisplayPaginatedPrompts_FitsOnePage(t *testing.T) {
	prompts := make([]*models.PromptResponse, 3)
	for i := range prompts {
		prompts[i] = &models.PromptResponse{
			Prompt: models.PromptJSON{
				Name:    "prompt",
				Version: "1.0.0",
			},
		}
	}

	// total <= pageSize should print all without pagination.
	displayPaginatedPrompts(prompts, 10, false)
}
